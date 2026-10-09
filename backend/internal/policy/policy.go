// Package policy is the deterministic policy engine. No LLM, no I/O, pure functions. Owner: agent B.
//
// Rules (FR5 + MVP decisions), evaluated per line then per order:
//
//	Line REJECT          : qty < 1 or no available/valid offer (NO_OFFER); offer merchant is not an
//	                       allowed vendor, or the offer's vendor_id disagrees with the vendor passed in
//	                       (VENDOR_NOT_ALLOWED); offer currency != policy currency, empty included
//	                       (CURRENCY_MISMATCH).
//	Line NEEDS_APPROVAL  : off-list, or the catalog item is inactive (OFF_LIST); unit price > catalog
//	                       ceiling (OVER_UNIT_CEILING); catalog item auto_approve=false (NOT_AUTO_APPROVE).
//	Line AUTO_APPROVE    : otherwise.
//	Order total          : sum of line totals of non-REJECT lines. A line total is the offer's
//	                       LandedCostCents, or unit*qty+shipping when that is larger (fail closed if
//	                       the caller forgot to compute landed cost). Arithmetic saturates, never wraps.
//	Order NEEDS_APPROVAL : total > per_order_limit (OVER_ORDER_LIMIT); month_to_date + total >
//	                       monthly_budget (OVER_MONTHLY_BUDGET); or any line NEEDS_APPROVAL.
//	Order REJECT         : every line is REJECT (or there are no lines).
//	Order AUTO_APPROVE   : otherwise.
//
// Limits are inclusive: a total exactly equal to a limit is allowed. A negative month-to-date is
// treated as zero so refunds never loosen the budget check. Only the reason codes in domain (and the
// openapi ReasonCode enum) are used; messages are generated here, never by an LLM.
package policy

import (
	"fmt"
	"math"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// LineInput is one line to evaluate.
type LineInput struct {
	LineItemID  string
	Description string // human label for reason messages; falls back to catalog name, offer title, id
	Qty         int
	CatalogItem *domain.CatalogItem // nil = off-list
	Offer       *domain.Offer       // selected (best) offer; nil = none found
	Vendor      *domain.Vendor      // vendor of the offer; nil = merchant not in vendors table
}

// Input is everything the engine needs. The caller loads it; the engine does no I/O.
type Input struct {
	Lines            []LineInput
	Config           domain.PolicyConfig
	MonthToDateCents domain.Cents // completed spend this month before this order
}

// LineResult is the decision for one line.
type LineResult struct {
	LineItemID string          `json:"line_item_id"`
	Decision   domain.Decision `json:"decision"`
	Reasons    []domain.Reason `json:"reasons"`
	TotalCents domain.Cents    `json:"total_cents"` // line total counted toward the order (0 if REJECT)
}

// Result is the overall decision.
type Result struct {
	Decision   domain.Decision `json:"decision"`
	Reasons    []domain.Reason `json:"reasons"` // order-level reasons plus a copy of line reasons
	Lines      []LineResult    `json:"lines"`
	TotalCents domain.Cents    `json:"total_cents"` // order total used for limit checks
}

// Evaluate applies the rules. It is pure and deterministic: the same input gives the same output.
func Evaluate(in Input) Result {
	currency := in.Config.Currency
	if currency == "" {
		currency = domain.Currency
	}

	res := Result{Lines: make([]LineResult, 0, len(in.Lines)), Reasons: []domain.Reason{}}
	var total domain.Cents
	anyNeeds, allReject := false, true

	for _, l := range in.Lines {
		lr := evaluateLine(l, currency)
		res.Lines = append(res.Lines, lr)
		switch lr.Decision {
		case domain.DecisionReject:
			continue
		case domain.DecisionNeedsApproval:
			anyNeeds = true
		}
		allReject = false
		total = satAdd(total, lr.TotalCents)
	}
	res.TotalCents = total

	if allReject {
		res.Decision = domain.DecisionReject
		if len(in.Lines) == 0 {
			res.Reasons = append(res.Reasons, domain.Reason{Code: domain.ReasonNoOffer, Message: "The request has no line items to order."})
		}
		for _, lr := range res.Lines {
			res.Reasons = append(res.Reasons, lr.Reasons...)
		}
		return res
	}

	var orderReasons []domain.Reason
	if total > in.Config.PerOrderLimitCents {
		orderReasons = append(orderReasons, domain.Reason{
			Code: domain.ReasonOverOrderLimit,
			Message: fmt.Sprintf("Order total %s is above the per-order limit of %s.",
				total.SGD(), in.Config.PerOrderLimitCents.SGD()),
		})
	}
	mtd := in.MonthToDateCents
	if mtd < 0 {
		mtd = 0
	}
	if projected := satAdd(mtd, total); projected > in.Config.MonthlyBudgetCents {
		orderReasons = append(orderReasons, domain.Reason{
			Code: domain.ReasonOverMonthlyBudget,
			Message: fmt.Sprintf("Spend this month would reach %s (%s already spent + %s), above the monthly budget of %s.",
				projected.SGD(), mtd.SGD(), total.SGD(), in.Config.MonthlyBudgetCents.SGD()),
		})
	}

	res.Reasons = append(res.Reasons, orderReasons...)
	for _, lr := range res.Lines {
		res.Reasons = append(res.Reasons, lr.Reasons...)
	}
	if anyNeeds || len(orderReasons) > 0 {
		res.Decision = domain.DecisionNeedsApproval
	} else {
		res.Decision = domain.DecisionAutoApprove
	}
	return res
}

func evaluateLine(l LineInput, currency string) LineResult {
	lr := LineResult{LineItemID: l.LineItemID, Reasons: []domain.Reason{}}
	label := lineLabel(l)
	reject := func(code domain.ReasonCode, msg string) LineResult {
		lr.Decision = domain.DecisionReject
		lr.Reasons = append(lr.Reasons, domain.Reason{Code: code, Message: msg})
		lr.TotalCents = 0
		return lr
	}

	// Hard gates (REJECT). Order matters only for which single reason is reported.
	if l.Qty < 1 {
		return reject(domain.ReasonNoOffer, fmt.Sprintf("%s: quantity %d is not orderable (must be at least 1).", label, l.Qty))
	}
	o := l.Offer
	if o == nil || !o.Available {
		return reject(domain.ReasonNoOffer, fmt.Sprintf("%s: no available offer was found at an allowed vendor.", label))
	}
	if o.UnitPriceCents < 0 || o.LandedCostCents < 0 || o.ShippingCents < 0 {
		return reject(domain.ReasonNoOffer, fmt.Sprintf("%s: the offer has an invalid (negative) price.", label))
	}
	if l.Vendor == nil || !l.Vendor.Allowed || (o.VendorID != nil && *o.VendorID != l.Vendor.ID) {
		merchant := o.MerchantName
		if merchant == "" {
			merchant = "unknown merchant"
		}
		return reject(domain.ReasonVendorNotAllowed, fmt.Sprintf("%s: %s is not an allowed vendor.", label, merchant))
	}
	if !strings.EqualFold(o.Currency, currency) {
		got := o.Currency
		if got == "" {
			got = "no currency"
		}
		return reject(domain.ReasonCurrencyMismatch, fmt.Sprintf("%s: offer is priced in %s, not %s.", label, got, currency))
	}

	lr.TotalCents = lineTotal(o, l.Qty)
	lr.Decision = domain.DecisionAutoApprove
	needs := func(code domain.ReasonCode, msg string) {
		lr.Decision = domain.DecisionNeedsApproval
		lr.Reasons = append(lr.Reasons, domain.Reason{Code: code, Message: msg})
	}

	c := l.CatalogItem
	if c == nil || !c.Active {
		needs(domain.ReasonOffList, fmt.Sprintf("%s is not on the approved catalog.", label))
		return lr
	}
	if o.UnitPriceCents > c.MaxUnitPriceCents {
		needs(domain.ReasonOverUnitCeiling, fmt.Sprintf("%s: unit price %s is above the catalog ceiling of %s.",
			label, o.UnitPriceCents.SGD(), c.MaxUnitPriceCents.SGD()))
	}
	if !c.AutoApprove {
		needs(domain.ReasonNotAutoApprove, fmt.Sprintf("%s always needs approval (catalog item is not auto-approved).", label))
	}
	return lr
}

// lineTotal is the landed cost, or unit*qty+shipping if that is larger (fail closed).
func lineTotal(o *domain.Offer, qty int) domain.Cents {
	computed := satAdd(satMul(o.UnitPriceCents, int64(qty)), o.ShippingCents)
	if o.LandedCostCents > computed {
		return o.LandedCostCents
	}
	return computed
}

func lineLabel(l LineInput) string {
	switch {
	case l.Description != "":
		return l.Description
	case l.CatalogItem != nil && l.CatalogItem.Name != "":
		return l.CatalogItem.Name
	case l.Offer != nil && l.Offer.Title != "":
		return l.Offer.Title
	case l.LineItemID != "":
		return "This item" // never show internal ids to users
	default:
		return "Line item"
	}
}

// satAdd adds two non-negative amounts, saturating at MaxInt64.
func satAdd(a, b domain.Cents) domain.Cents {
	if b > 0 && a > domain.Cents(math.MaxInt64)-b {
		return domain.Cents(math.MaxInt64)
	}
	return a + b
}

// satMul multiplies a non-negative amount by a non-negative qty, saturating at MaxInt64.
func satMul(a domain.Cents, n int64) domain.Cents {
	if a == 0 || n == 0 {
		return 0
	}
	if int64(a) > math.MaxInt64/n {
		return domain.Cents(math.MaxInt64)
	}
	return a * domain.Cents(n)
}

// DriftExceeded reports whether live exceeds approved by more than pct percent.
// e.g. approved 10000, live 10500, pct 5 -> false (exactly 5% is allowed); live 10501 -> true.
// A price decrease never exceeds. A negative or NaN pct is treated as 0 (fail closed).
func DriftExceeded(approvedCents, liveCents domain.Cents, pct float64) bool {
	if liveCents <= approvedCents {
		return false
	}
	if math.IsNaN(pct) || pct < 0 {
		pct = 0
	}
	if math.IsInf(pct, 1) {
		return false
	}
	// Compare (live-approved)*100 > pct*approved. Use big-enough float math; cents fit in 2^53.
	return float64(liveCents-approvedCents)*100 > pct*float64(approvedCents)
}

// DriftPct returns the percentage increase of live over approved (negative for a decrease).
// approved <= 0 returns 0 when live <= 0, else +Inf.
func DriftPct(approvedCents, liveCents domain.Cents) float64 {
	if approvedCents <= 0 {
		if liveCents <= 0 {
			return 0
		}
		return math.Inf(1)
	}
	return float64(liveCents-approvedCents) * 100 / float64(approvedCents)
}

// CheckDrift is the second gate at checkout: it compares the live Reap total with the approved total.
// It returns ok=false and a PRICE_DRIFT reason when the live total rose by more than cfg.PriceDriftPct.
func CheckDrift(approvedCents, liveCents domain.Cents, cfg domain.PolicyConfig) (ok bool, reason domain.Reason) {
	if !DriftExceeded(approvedCents, liveCents, cfg.PriceDriftPct) {
		return true, domain.Reason{}
	}
	pct := DriftPct(approvedCents, liveCents)
	pctStr := "an unknown amount"
	if !math.IsInf(pct, 0) {
		pctStr = fmt.Sprintf("%.2f%%", pct)
	}
	return false, domain.Reason{
		Code: domain.ReasonPriceDrift,
		Message: fmt.Sprintf("Live checkout total %s is %s above the approved %s (threshold %g%%).",
			liveCents.SGD(), pctStr, approvedCents.SGD(), cfg.PriceDriftPct),
	}
}
