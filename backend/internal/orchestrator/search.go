package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/policy"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// OpenSearchLabel is the vendor_domain reported in SSE/audit for the open (allow-list filtered) search.
const OpenSearchLabel = "open"

// SearchTask is one planned vendor search for one line item.
type SearchTask struct {
	LineItemID string
	Query      agents.SearchQuery
	Label      string // vendor domain or OpenSearchLabel
}

// PlanSearch builds the search plan for one line item: the catalog item's preferred vendors first
// (merchantPreference ONLY each, in preference order), then for off-list items the most relevant
// office vendors (by keyword overlap with the vendor's category/notes, then priority), and finally
// one open search filtered to the allow-list. Pure.
func PlanSearch(li domain.LineItem, item *domain.CatalogItem, allowed []domain.Vendor, perVendor, maxOffList int) []SearchTask {
	byID := map[string]domain.Vendor{}
	for _, v := range allowed {
		if v.Allowed {
			byID[v.ID] = v
		}
	}
	searchable := func(v domain.Vendor) bool { return v.Allowed && v.ReapMerchantName != "" && v.Domain != "" }
	query, unit := li.Description, ""
	if item != nil {
		if item.SearchQuery != "" {
			query = item.SearchQuery
		}
		unit = item.Unit
	}
	base := agents.SearchQuery{LineItemID: li.ID, Query: query, Qty: li.Qty, Limit: perVendor, CatalogUnit: unit}

	var tasks []SearchTask
	seen := map[string]bool{}
	add := func(v domain.Vendor) {
		if seen[v.ID] || !searchable(v) {
			return
		}
		seen[v.ID] = true
		q := base
		q.Vendor = v
		tasks = append(tasks, SearchTask{LineItemID: li.ID, Query: q, Label: v.Domain})
	}
	if item != nil {
		for _, id := range item.PreferredVendorIDs {
			if v, ok := byID[id]; ok {
				add(v)
			}
		}
	} else {
		want := agents.Tokens(li.Description)
		type scored struct {
			v     domain.Vendor
			score int
		}
		var cands []scored
		for _, v := range allowed {
			if !v.OfficeRelevant || !searchable(v) {
				continue
			}
			s := 0
			for t := range agents.Tokens(v.Name + " " + v.Category + " " + v.Notes) {
				if want[t] {
					s++
				}
			}
			cands = append(cands, scored{v, s})
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].score != cands[j].score {
				return cands[i].score > cands[j].score
			}
			if cands[i].v.Priority != cands[j].v.Priority {
				return cands[i].v.Priority < cands[j].v.Priority
			}
			return cands[i].v.Domain < cands[j].v.Domain
		})
		for _, c := range cands {
			if len(tasks) >= maxOffList {
				break
			}
			add(c.v)
		}
	}
	open := base
	open.Open = true
	open.Allowed = allowed
	tasks = append(tasks, SearchTask{LineItemID: li.ID, Query: open, Label: OpenSearchLabel})
	return tasks
}

// VendorPriority maps vendor id -> ranking preference (lower is better): the catalog item's
// preferred order first, then 100 + the vendor's global priority.
func VendorPriority(item *domain.CatalogItem, vendors []domain.Vendor) map[string]int {
	m := make(map[string]int, len(vendors))
	for _, v := range vendors {
		m[v.ID] = 100 + v.Priority
	}
	if item != nil {
		for i, id := range item.PreferredVendorIDs {
			m[id] = i
		}
	}
	return m
}

type searchResult struct {
	task   SearchTask
	offers []domain.Offer
	err    error
}

// handleSearch fans out vendor searches for every line item under the search deadline, ranks what
// arrived, runs policy and moves the request to quoted (or rejected). Partial results are fine.
func (o *Impl) handleSearch(ctx context.Context, job queue.Job) error {
	r, err := o.d.Store.Requests().Get(ctx, job.RequestID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return queue.Permanent(err)
		}
		return err
	}
	if r.Status != domain.StatusSearching {
		return nil
	}
	lines, err := o.d.Store.LineItems().ListByRequest(ctx, r.ID)
	if err != nil {
		return err
	}
	vendors, err := o.d.Store.Vendors().List(ctx, store.VendorFilter{AllowedOnly: true})
	if err != nil {
		return err
	}
	vendors = o.onAllowList(vendors) // never search a merchant that is off the allow-list
	items, err := o.catalogFor(ctx, lines)
	if err != nil {
		return err
	}

	var tasks []SearchTask
	for _, li := range lines {
		lt := PlanSearch(li, items[li.ID], vendors, o.d.ResultsPerVendor, o.d.MaxOffListVendors)
		labels := make([]string, 0, len(lt))
		for _, t := range lt {
			labels = append(labels, t.Label)
		}
		o.publish(events.SearchStarted, r.ID, map[string]any{"request_id": r.ID, "line_item_id": li.ID, "vendors": labels})
		tasks = append(tasks, lt...)
	}

	results := o.fanOut(ctx, r.ID, tasks)
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err() // runner shutting down; retry later
	}
	return o.rankAndEvaluate(ctx, r, lines, items, vendors, results)
}

// fanOut runs tasks in parallel (bounded by the global semaphore) until all finish or the search
// deadline passes. Results that arrive after the deadline are dropped.
func (o *Impl) fanOut(ctx context.Context, requestID string, tasks []SearchTask) map[string][]domain.Offer {
	sctx, cancel := context.WithTimeout(ctx, o.d.SearchDeadline)
	defer cancel()
	ch := make(chan searchResult, len(tasks))
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(t SearchTask) {
			defer wg.Done()
			select {
			case o.sem <- struct{}{}:
			case <-sctx.Done():
				ch <- searchResult{task: t, err: sctx.Err()}
				return
			}
			defer func() { <-o.sem }()
			offers, err := o.d.Adapter.Search(sctx, t.Query)
			ch <- searchResult{task: t, offers: offers, err: err}
		}(t)
	}
	// Do not wait for stragglers past the deadline; they drain into the buffered channel.
	go func() { wg.Wait() }()

	out := map[string][]domain.Offer{}
	for received := 0; received < len(tasks); received++ {
		var res searchResult
		select {
		case res = <-ch:
		case <-sctx.Done():
			// Collect anything already delivered, then stop.
		drain:
			for {
				select {
				case res = <-ch:
					o.noteResult(ctx, requestID, res, out)
				default:
					break drain
				}
			}
			o.log.Info("search deadline reached", "request_id", requestID, "received", received, "tasks", len(tasks))
			return out
		}
		o.noteResult(ctx, requestID, res, out)
	}
	return out
}

func (o *Impl) noteResult(ctx context.Context, requestID string, res searchResult, out map[string][]domain.Offer) {
	data := map[string]any{"request_id": requestID, "line_item_id": res.task.LineItemID, "vendor_domain": res.task.Label, "offers_found": len(res.offers)}
	if res.err != nil {
		data["error"] = res.err.Error()
	} else {
		out[res.task.LineItemID] = append(out[res.task.LineItemID], res.offers...)
	}
	o.publish(events.SearchVendorResult, requestID, data)
	o.record(ctx, requestID, domain.ActorAgent, "search:"+res.task.Label, audit.SearchCompleted, data)
}

func (o *Impl) catalogFor(ctx context.Context, lines []domain.LineItem) (map[string]*domain.CatalogItem, error) {
	out := map[string]*domain.CatalogItem{}
	for _, li := range lines {
		if li.CatalogItemID == nil {
			continue
		}
		it, err := o.d.Store.Catalog().Get(ctx, *li.CatalogItemID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue // deleted from catalog since parsing: treat as off-list
			}
			return nil, err
		}
		out[li.ID] = &it
	}
	return out, nil
}

// rankAndEvaluate persists ranked offers, selects the best per line, runs policy and moves the
// request to quoted (or rejected when every line is REJECT).
func (o *Impl) rankAndEvaluate(ctx context.Context, r domain.PurchaseRequest, lines []domain.LineItem, items map[string]*domain.CatalogItem,
	vendors []domain.Vendor, results map[string][]domain.Offer) error {
	allowed := map[string]bool{}
	for _, v := range vendors {
		if o.vendorAllowed(v) {
			allowed[v.ID] = true
		}
	}
	type ranked struct {
		line   domain.LineItem
		offers []*domain.Offer
	}
	deduped := make(map[string][]domain.Offer, len(lines))
	for _, li := range lines {
		deduped[li.ID] = dedupeAllowed(results[li.ID], allowed)
	}
	relevant := o.filterAllRelevant(ctx, r.ID, lines, items, deduped)
	var all []ranked
	for _, li := range lines {
		rk := RankOffers(relevant[li.ID], li.Qty, VendorPriority(items[li.ID], vendors))
		ptrs := make([]*domain.Offer, 0, len(rk))
		for i := range rk {
			rk[i].LineItemID = li.ID
			ptrs = append(ptrs, &rk[i])
		}
		all = append(all, ranked{li, ptrs})
	}

	cfg, err := o.d.Store.Policy().Get(ctx)
	if err != nil {
		return fmt.Errorf("load policy: %w", err)
	}
	mtd, err := o.committedSpend(ctx, r.ID)
	if err != nil {
		return fmt.Errorf("month to date spend: %w", err)
	}

	var res policy.Result
	err = o.d.Store.WithTx(ctx, func(tx store.Store) error {
		in := policy.Input{Config: cfg, MonthToDateCents: mtd}
		for i := range all {
			if err := tx.Offers().ReplaceForLineItem(ctx, all[i].line.ID, all[i].offers); err != nil {
				return fmt.Errorf("save offers: %w", err)
			}
			in.Lines = append(in.Lines, lineInput(all[i].line, items[all[i].line.ID], best(all[i].offers), vendors))
		}
		res = o.d.Evaluate(in)
		decisions := map[string]policy.LineResult{}
		for _, lr := range res.Lines {
			decisions[lr.LineItemID] = lr
		}
		var subtotal, shipping domain.Cents
		for i := range all {
			li := all[i].line
			lr := decisions[li.ID]
			li.PolicyDecision, li.Reasons = lr.Decision, nonNil(lr.Reasons)
			li.SelectedOfferID = nil
			if b := best(all[i].offers); b != nil {
				id := b.ID
				li.SelectedOfferID = &id
				if lr.Decision != domain.DecisionReject {
					subtotal += b.UnitPriceCents.MulQty(agents.PurchaseQty(li.Qty, b.PackSize))
					shipping += b.ShippingCents
				}
			}
			if err := tx.LineItems().Update(ctx, &li); err != nil {
				return fmt.Errorf("update line item: %w", err)
			}
			all[i].line = li
		}
		cur, err := tx.Requests().Get(ctx, r.ID)
		if err != nil {
			return err
		}
		cur.SubtotalCents, cur.ShippingCents, cur.TotalCents, cur.Decision = subtotal, shipping, res.TotalCents, res.Decision
		return tx.Requests().Update(ctx, &cur)
	})
	if err != nil {
		return err
	}
	for _, a := range all {
		top := make([]domain.Offer, 0, 3)
		for _, of := range a.offers {
			if len(top) == 3 {
				break
			}
			top = append(top, *of)
		}
		o.publish(events.OffersRanked, r.ID, map[string]any{"request_id": r.ID, "line_item_id": a.line.ID, "offers": top})
	}
	o.record(ctx, r.ID, domain.ActorSystem, "policy", audit.PolicyEvaluated, res)
	o.publish(events.PolicyEvaluated, r.ID, map[string]any{
		"request_id": r.ID, "decision": res.Decision, "reasons": nonNil(res.Reasons), "lines": res.Lines, "total_cents": res.TotalCents,
	})
	if err := o.transition(ctx, r.ID, domain.StatusSearching, domain.StatusQuoted, "", domain.ActorSystem, ""); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalidTransition) {
			return nil
		}
		return err
	}
	if res.Decision == domain.DecisionReject {
		return o.transition(ctx, r.ID, domain.StatusQuoted, domain.StatusRejected, "", domain.ActorSystem, "policy")
	}
	return nil
}

// lineInput builds the policy input for one line. Qty is the number of purchasable variants, so
// the engine's unit*qty matches what will actually be bought.
func lineInput(li domain.LineItem, item *domain.CatalogItem, offer *domain.Offer, vendors []domain.Vendor) policy.LineInput {
	in := policy.LineInput{LineItemID: li.ID, Description: li.Description, Qty: li.Qty, CatalogItem: item}
	if offer != nil {
		of := *offer
		in.Offer = &of
		in.Qty = agents.PurchaseQty(li.Qty, of.PackSize)
		if of.VendorID != nil {
			for i := range vendors {
				if vendors[i].ID == *of.VendorID {
					v := vendors[i]
					in.Vendor = &v
					break
				}
			}
		}
	}
	return in
}

// onAllowList drops vendors whose domain is not on the merchant allow-list.
func (o *Impl) onAllowList(vs []domain.Vendor) []domain.Vendor {
	out := vs[:0:0]
	for _, v := range vs {
		if o.allow.Contains(v.Domain) {
			out = append(out, v)
		}
	}
	return out
}

// withAllowList returns copies of vs where a vendor off the allow-list is not allowed (policy
// then fails closed with VENDOR_NOT_ALLOWED).
func (o *Impl) withAllowList(vs []domain.Vendor) []domain.Vendor {
	out := make([]domain.Vendor, len(vs))
	for i, v := range vs {
		v.Allowed = o.vendorAllowed(v)
		out[i] = v
	}
	return out
}

func best(offers []*domain.Offer) *domain.Offer {
	for _, o := range offers {
		if o.Available {
			return o
		}
	}
	return nil
}

// dedupeAllowed drops offers from non-allowed vendors (defence in depth) and duplicates (the same
// variant found by both the vendor search and the open search).
func dedupeAllowed(offers []domain.Offer, allowed map[string]bool) []domain.Offer {
	seen := map[string]bool{}
	out := make([]domain.Offer, 0, len(offers))
	for _, of := range offers {
		if of.VendorID == nil || !allowed[*of.VendorID] {
			continue
		}
		key := of.ReapVariantID
		if key == "" {
			key = of.ReapProductID + "|" + of.Title
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if strings.TrimSpace(of.Currency) == "" {
			of.Currency = domain.Currency
		}
		out = append(out, of)
	}
	return out
}

func nonNil(rs []domain.Reason) []domain.Reason {
	if rs == nil {
		return []domain.Reason{}
	}
	return rs
}
