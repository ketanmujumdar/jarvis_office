package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

func (s *Server) st(w http.ResponseWriter, r *http.Request) bool {
	if err := need(s.d.Store != nil, "store"); err != nil {
		s.fail(w, r, err)
		return false
	}
	return true
}

// OrderSummary is one row of GET /orders.
type OrderSummary struct {
	Request   domain.PurchaseRequest `json:"request"`
	Requester *domain.User           `json:"requester,omitempty"`
	Approver  *domain.User           `json:"approver,omitempty"`
	Vendors   []string               `json:"vendors"`
	ItemCount int                    `json:"item_count"`
	Payments  []domain.Payment       `json:"payments"`
}

// orderStatuses are "reached checkout or later". Failed requests are included only when they got
// as far as creating a payment.
var orderStatuses = []domain.RequestStatus{
	domain.StatusCheckingOut, domain.StatusAwaitingPayment, domain.StatusPaying, domain.StatusOrdered,
	domain.StatusFailed,
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	reqs, err := s.d.Store.Requests().List(ctx, store.RequestFilter{Statuses: orderStatuses, Limit: limit, Offset: offset})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	users := map[string]*domain.User{}
	lookup := func(id string) *domain.User {
		if id == "" {
			return nil
		}
		if u, ok := users[id]; ok {
			return u
		}
		var p *domain.User
		if u, err := s.d.Store.Users().Get(ctx, id); err == nil {
			p = &u
		}
		users[id] = p
		return p
	}
	out := make([]OrderSummary, 0, len(reqs))
	for _, pr := range reqs {
		d, err := s.d.Store.Requests().Detail(ctx, pr.ID)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d = normalizeDetail(d)
		if d.Request.Status == domain.StatusFailed && len(d.Payments) == 0 {
			continue
		}
		o := OrderSummary{Request: d.Request, Requester: lookup(d.Request.RequesterID), Payments: d.Payments,
			ItemCount: len(d.LineItems), Vendors: vendorsOf(d)}
		for i := len(d.Approvals) - 1; i >= 0; i-- {
			a := d.Approvals[i]
			if a.Status == domain.ApprovalApproved && a.ApproverID != nil {
				o.Approver = lookup(*a.ApproverID)
				break
			}
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

// vendorsOf lists merchant names: from payments, else from the selected offers.
func vendorsOf(d domain.RequestDetail) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, p := range d.Payments {
		add(p.MerchantName)
	}
	if len(out) > 0 {
		return out
	}
	selected := map[string]bool{}
	for _, li := range d.LineItems {
		if li.SelectedOfferID != nil {
			selected[*li.SelectedOfferID] = true
		}
	}
	for _, o := range d.Offers {
		if selected[o.ID] {
			add(o.MerchantName)
		}
	}
	return out
}

// SpendSummary is GET /spend/monthly.
type SpendSummary struct {
	Currency           string              `json:"currency"`
	MonthlyBudgetCents domain.Cents        `json:"monthly_budget_cents"`
	MonthToDateCents   domain.Cents        `json:"month_to_date_cents"`
	Months             []domain.MonthSpend `json:"months"`
}

func (s *Server) monthlySpend(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	months := 6
	if v := r.URL.Query().Get("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 24 {
			s.fail(w, r, validationf("months must be an integer between 1 and 24"))
			return
		}
		months = n
	}
	ctx := r.Context()
	now := s.d.Clock()
	out := SpendSummary{Currency: domain.Currency}
	pol, err := s.d.Store.Policy().Get(ctx)
	switch {
	case err == nil:
		out.MonthlyBudgetCents = pol.MonthlyBudgetCents
		if pol.Currency != "" {
			out.Currency = pol.Currency
		}
	case errors.Is(err, domain.ErrNotFound):
	default:
		s.fail(w, r, err)
		return
	}
	if out.MonthToDateCents, err = s.d.Store.Spend().MonthToDate(ctx, now); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.Months, err = s.d.Store.Spend().ByMonth(ctx, months, now); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.Months == nil {
		out.Months = []domain.MonthSpend{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) globalAudit(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	evs, err := s.d.Store.Audit().List(r.Context(), store.AuditFilter{
		Type: strings.TrimSpace(r.URL.Query().Get("type")), Limit: limit, Offset: offset,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": nonNilAudit(evs)})
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	q := r.URL.Query()
	items, err := s.d.Store.Catalog().List(r.Context(), store.CatalogFilter{
		ActiveOnly: true, Category: strings.TrimSpace(q.Get("category")), Query: strings.TrimSpace(q.Get("q")),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilCatalog(items)})
}

func nonNilCatalog(items []domain.CatalogItem) []domain.CatalogItem {
	if items == nil {
		items = []domain.CatalogItem{}
	}
	for i := range items {
		if items[i].Aliases == nil {
			items[i].Aliases = []string{}
		}
		if items[i].PreferredVendorIDs == nil {
			items[i].PreferredVendorIDs = []string{}
		}
	}
	return items
}

func (s *Server) listAddresses(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	as, err := s.d.Store.Addresses().List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if as == nil {
		as = []domain.Address{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": as})
}
