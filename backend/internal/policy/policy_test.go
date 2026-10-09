package policy

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// ---------- fixtures ----------

var cfg = domain.PolicyConfig{
	Currency:           "SGD",
	PerOrderLimitCents: 50000,  // S$500
	MonthlyBudgetCents: 300000, // S$3,000
	PriceDriftPct:      5,
}

func strp(s string) *string { return &s }

func vendor(id string, allowed bool) *domain.Vendor {
	return &domain.Vendor{ID: id, Domain: id + ".sg", Name: id, ReapMerchantName: id + " SG", Allowed: allowed}
}

func item(maxCents domain.Cents, auto bool) *domain.CatalogItem {
	return &domain.CatalogItem{ID: "cat-1", SKU: "a4", Name: "A4 Paper", MaxUnitPriceCents: maxCents, AutoApprove: auto, Active: true}
}

// offer builds an available SGD offer from vendor v1 with landed = unit*qty + ship.
func offer(unit domain.Cents, qty int, ship domain.Cents) *domain.Offer {
	return &domain.Offer{
		ID: "off-1", VendorID: strp("v1"), MerchantName: "v1 SG", Title: "Some Product",
		UnitPriceCents: unit, Currency: "SGD", PackSize: 1, ShippingCents: ship,
		LandedCostCents: unit.MulQty(qty) + ship, Available: true, Rank: 1,
	}
}

// line is an on-list, auto-approvable line: ceiling S$9.00, unit price given.
func line(id string, unit domain.Cents, qty int) LineInput {
	return LineInput{LineItemID: id, Qty: qty, CatalogItem: item(900, true), Offer: offer(unit, qty, 0), Vendor: vendor("v1", true)}
}

func with(l LineInput, f func(*LineInput)) LineInput { f(&l); return l }

func codes(rs []domain.Reason) []domain.ReasonCode {
	out := []domain.ReasonCode{}
	for _, r := range rs {
		out = append(out, r.Code)
	}
	return out
}

// ---------- line-level rules ----------

func TestEvaluateLineRules(t *testing.T) {
	tests := []struct {
		name      string
		line      LineInput
		want      domain.Decision
		wantCodes []domain.ReasonCode
		wantTotal domain.Cents
	}{
		{"on list under ceiling", line("l1", 710, 10), domain.DecisionAutoApprove, nil, 7100},
		{"unit price exactly at ceiling", line("l1", 900, 10), domain.DecisionAutoApprove, nil, 9000},
		{"unit price one cent over ceiling", line("l1", 901, 10), domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverUnitCeiling}, 9010},
		{"zero ceiling, positive price", with(line("l1", 1, 1), func(l *LineInput) { l.CatalogItem.MaxUnitPriceCents = 0 }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverUnitCeiling}, 1},
		{"free item at zero ceiling", with(line("l1", 0, 1), func(l *LineInput) { l.CatalogItem.MaxUnitPriceCents = 0 }),
			domain.DecisionAutoApprove, nil, 0},
		{"off-list", with(line("l1", 710, 1), func(l *LineInput) { l.CatalogItem = nil }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOffList}, 710},
		{"off-list skips ceiling check", with(line("l1", 99999, 1), func(l *LineInput) { l.CatalogItem = nil }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOffList}, 99999},
		{"inactive catalog item is off-list", with(line("l1", 710, 1), func(l *LineInput) { l.CatalogItem.Active = false }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOffList}, 710},
		{"not auto-approve", with(line("l1", 710, 1), func(l *LineInput) { l.CatalogItem.AutoApprove = false }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonNotAutoApprove}, 710},
		{"over ceiling and not auto-approve", with(line("l1", 1000, 1), func(l *LineInput) { l.CatalogItem.AutoApprove = false }),
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverUnitCeiling, domain.ReasonNotAutoApprove}, 1000},
		{"zero qty", line("l1", 710, 0), domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"negative qty", line("l1", 710, -3), domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"no offer", with(line("l1", 710, 1), func(l *LineInput) { l.Offer = nil }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"offer unavailable", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.Available = false }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"negative price", with(line("l1", -1, 1), func(l *LineInput) {}),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"negative shipping", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.ShippingCents = -100 }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"unknown vendor", with(line("l1", 710, 1), func(l *LineInput) { l.Vendor = nil }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonVendorNotAllowed}, 0},
		{"vendor not allowed", with(line("l1", 710, 1), func(l *LineInput) { l.Vendor.Allowed = false }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonVendorNotAllowed}, 0},
		{"offer vendor id disagrees with vendor", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.VendorID = strp("other") }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonVendorNotAllowed}, 0},
		{"offer without vendor id but allowed vendor", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.VendorID = nil }),
			domain.DecisionAutoApprove, nil, 710},
		{"vendor not allowed beats off-list", with(line("l1", 710, 1), func(l *LineInput) { l.Vendor.Allowed = false; l.CatalogItem = nil }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonVendorNotAllowed}, 0},
		{"currency USD", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.Currency = "USD" }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonCurrencyMismatch}, 0},
		{"currency empty fails closed", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.Currency = "" }),
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonCurrencyMismatch}, 0},
		{"currency lower-case sgd ok", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.Currency = "sgd" }),
			domain.DecisionAutoApprove, nil, 710},
		{"landed cost includes shipping", with(line("l1", 710, 2), func(l *LineInput) { l.Offer = offer(710, 2, 500) }),
			domain.DecisionAutoApprove, nil, 1920},
		{"landed cost missing uses unit*qty+ship", with(line("l1", 710, 3), func(l *LineInput) { l.Offer.LandedCostCents = 0; l.Offer.ShippingCents = 300 }),
			domain.DecisionAutoApprove, nil, 2430},
		{"landed cost larger (tax) is kept", with(line("l1", 710, 1), func(l *LineInput) { l.Offer.LandedCostCents = 774 }),
			domain.DecisionAutoApprove, nil, 774},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(Input{Lines: []LineInput{tt.line}, Config: cfg})
			if len(got.Lines) != 1 {
				t.Fatalf("lines = %d, want 1", len(got.Lines))
			}
			lr := got.Lines[0]
			if lr.Decision != tt.want {
				t.Errorf("decision = %s, want %s (reasons %v)", lr.Decision, tt.want, lr.Reasons)
			}
			if gc := codes(lr.Reasons); !reflect.DeepEqual(gc, append([]domain.ReasonCode{}, tt.wantCodes...)) {
				t.Errorf("codes = %v, want %v", gc, tt.wantCodes)
			}
			if lr.TotalCents != tt.wantTotal {
				t.Errorf("line total = %d, want %d", lr.TotalCents, tt.wantTotal)
			}
			if lr.LineItemID != tt.line.LineItemID {
				t.Errorf("line id = %q", lr.LineItemID)
			}
			for _, r := range lr.Reasons {
				if strings.TrimSpace(r.Message) == "" {
					t.Errorf("reason %s has empty message", r.Code)
				}
			}
			// Single-line orders: order decision equals line decision when within limits.
			if got.Decision != tt.want {
				t.Errorf("order decision = %s, want %s", got.Decision, tt.want)
			}
		})
	}
}

// ---------- order-level rules ----------

func TestEvaluateOrderRules(t *testing.T) {
	tests := []struct {
		name      string
		lines     []LineInput
		cfg       domain.PolicyConfig
		mtd       domain.Cents
		want      domain.Decision
		wantCodes []domain.ReasonCode // full Result.Reasons codes, order-level first
		wantTotal domain.Cents
	}{
		{"all auto within limits", []LineInput{line("a", 710, 10), line("b", 500, 2)}, cfg, 0,
			domain.DecisionAutoApprove, nil, 8100},
		{"total exactly at per-order limit", []LineInput{line("a", 500, 100)}, cfg, 0,
			domain.DecisionAutoApprove, nil, 50000},
		{"total one cent over per-order limit", []LineInput{line("a", 500, 100), with(line("b", 1, 1), func(l *LineInput) { l.LineItemID = "b" })}, cfg, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverOrderLimit}, 50001},
		{"monthly budget exactly reached", []LineInput{line("a", 500, 100)}, cfg, 250000,
			domain.DecisionAutoApprove, nil, 50000},
		{"monthly budget one cent over", []LineInput{line("a", 500, 100)}, cfg, 250001,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverMonthlyBudget}, 50000},
		{"budget already exhausted", []LineInput{line("a", 100, 1)}, cfg, 300000,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverMonthlyBudget}, 100},
		{"negative mtd treated as zero", []LineInput{line("a", 500, 100)}, domain.PolicyConfig{Currency: "SGD", PerOrderLimitCents: 50000, MonthlyBudgetCents: 49999}, -100000,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverMonthlyBudget}, 50000},
		{"over both limits", []LineInput{line("a", 900, 1000)}, cfg, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverOrderLimit, domain.ReasonOverMonthlyBudget}, 900000},
		{"one needs-approval line makes order need approval", []LineInput{line("a", 710, 1), with(line("b", 710, 1), func(l *LineInput) { l.CatalogItem = nil })}, cfg, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOffList}, 1420},
		{"reject lines are dropped and excluded from total", []LineInput{line("a", 710, 1), with(line("b", 40000, 2), func(l *LineInput) { l.Vendor.Allowed = false })}, cfg, 0,
			domain.DecisionAutoApprove, []domain.ReasonCode{domain.ReasonVendorNotAllowed}, 710},
		{"reject + needs => needs", []LineInput{line("a", 710, 0), with(line("b", 710, 1), func(l *LineInput) { l.CatalogItem.AutoApprove = false })}, cfg, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonNoOffer, domain.ReasonNotAutoApprove}, 710},
		{"every line reject", []LineInput{line("a", 710, 0), with(line("b", 710, 1), func(l *LineInput) { l.Offer = nil })}, cfg, 0,
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer, domain.ReasonNoOffer}, 0},
		{"no lines", nil, cfg, 0,
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonNoOffer}, 0},
		{"reject lines do not trigger order limits", []LineInput{with(line("a", 900000, 1), func(l *LineInput) { l.Offer.Currency = "USD" })}, cfg, 299999,
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonCurrencyMismatch}, 0},
		{"zero limits fail closed", []LineInput{line("a", 1, 1)}, domain.PolicyConfig{Currency: "SGD"}, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverOrderLimit, domain.ReasonOverMonthlyBudget}, 1},
		{"empty config currency defaults to SGD", []LineInput{line("a", 710, 1)}, domain.PolicyConfig{PerOrderLimitCents: 50000, MonthlyBudgetCents: 300000}, 0,
			domain.DecisionAutoApprove, nil, 710},
		{"config currency USD rejects SGD offer", []LineInput{line("a", 710, 1)}, domain.PolicyConfig{Currency: "USD", PerOrderLimitCents: 50000, MonthlyBudgetCents: 300000}, 0,
			domain.DecisionReject, []domain.ReasonCode{domain.ReasonCurrencyMismatch}, 0},
		{"huge qty saturates instead of overflowing", []LineInput{with(line("a", 900, 1), func(l *LineInput) {
			l.Qty = math.MaxInt64 / 10
			l.Offer.LandedCostCents = 0
		})}, cfg, 0,
			domain.DecisionNeedsApproval, []domain.ReasonCode{domain.ReasonOverOrderLimit, domain.ReasonOverMonthlyBudget}, domain.Cents(math.MaxInt64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(Input{Lines: tt.lines, Config: tt.cfg, MonthToDateCents: tt.mtd})
			if got.Decision != tt.want {
				t.Errorf("decision = %s, want %s (reasons %v)", got.Decision, tt.want, got.Reasons)
			}
			if gc := codes(got.Reasons); !reflect.DeepEqual(gc, append([]domain.ReasonCode{}, tt.wantCodes...)) {
				t.Errorf("codes = %v, want %v", gc, tt.wantCodes)
			}
			if got.TotalCents != tt.wantTotal {
				t.Errorf("total = %d, want %d", got.TotalCents, tt.wantTotal)
			}
			if len(got.Lines) != len(tt.lines) {
				t.Errorf("lines = %d, want %d", len(got.Lines), len(tt.lines))
			}
			if got.Reasons == nil {
				t.Error("Reasons must be non-nil for JSON")
			}
		})
	}
}

func TestEvaluateIsDeterministicAndDoesNotMutate(t *testing.T) {
	in := Input{Lines: []LineInput{line("a", 710, 10), with(line("b", 1000, 1), func(l *LineInput) { l.CatalogItem = nil })}, Config: cfg, MonthToDateCents: 1000}
	before := line("a", 710, 10)
	r1 := Evaluate(in)
	r2 := Evaluate(in)
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("non-deterministic:\n%+v\n%+v", r1, r2)
	}
	if !reflect.DeepEqual(*in.Lines[0].Offer, *before.Offer) || !reflect.DeepEqual(*in.Lines[0].CatalogItem, *before.CatalogItem) {
		t.Fatal("Evaluate mutated its input")
	}
}

func TestReasonMessagesAreHuman(t *testing.T) {
	l := with(line("a", 1000, 60), func(l *LineInput) { l.Description = "Printer paper" })
	got := Evaluate(Input{Lines: []LineInput{l}, Config: cfg, MonthToDateCents: 250000})
	joined := ""
	for _, r := range got.Reasons {
		joined += r.Message + "\n"
	}
	for _, want := range []string{"Printer paper", "S$10.00", "S$9.00", "S$600.00", "S$500.00", "S$3000.00", "S$2500.00"} {
		if !strings.Contains(joined, want) {
			t.Errorf("messages missing %q:\n%s", want, joined)
		}
	}
}

func TestLineLabelFallbacks(t *testing.T) {
	tests := []struct {
		name string
		l    LineInput
		want string
	}{
		{"description", LineInput{Description: "Desk", CatalogItem: &domain.CatalogItem{Name: "Cat"}}, "Desk"},
		{"catalog name", LineInput{CatalogItem: &domain.CatalogItem{Name: "Cat"}, Offer: &domain.Offer{Title: "T"}}, "Cat"},
		{"offer title", LineInput{Offer: &domain.Offer{Title: "T"}}, "T"},
		{"line id never shown", LineInput{LineItemID: "x1"}, "This item"},
		{"nothing", LineInput{}, "Line item"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lineLabel(tt.l); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

// ---------- drift ----------

func TestDriftExceeded(t *testing.T) {
	tests := []struct {
		name           string
		approved, live domain.Cents
		pct            float64
		want           bool
	}{
		{"equal", 10000, 10000, 5, false},
		{"decrease", 10000, 9000, 5, false},
		{"exactly 5pct", 10000, 10500, 5, false},
		{"just over 5pct", 10000, 10501, 5, true},
		{"just under 5pct", 10000, 10499, 5, false},
		{"zero pct any increase", 10000, 10001, 0, true},
		{"zero pct equal", 10000, 10000, 0, false},
		{"small amounts", 199, 209, 5, true},
		{"small amounts ok", 200, 210, 5, false},
		{"fractional pct exact", 10000, 10250, 2.5, false},
		{"fractional pct over", 10000, 10251, 2.5, true},
		{"approved zero, live positive", 0, 1, 5, true},
		{"approved zero, live zero", 0, 0, 5, false},
		{"negative pct treated as zero", 10000, 10001, -5, true},
		{"NaN pct treated as zero", 10000, 10001, math.NaN(), true},
		{"large amounts exactly 5pct", 1_000_000_000, 1_050_000_000, 5, false},
		{"large amounts over 5pct", 1_000_000_000, 1_050_000_001, 5, true},
		{"100pct doubling allowed", 5000, 10000, 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DriftExceeded(tt.approved, tt.live, tt.pct); got != tt.want {
				t.Errorf("DriftExceeded(%d,%d,%v) = %v, want %v", tt.approved, tt.live, tt.pct, got, tt.want)
			}
		})
	}
}

func TestDriftPct(t *testing.T) {
	tests := []struct {
		approved, live domain.Cents
		want           float64
	}{
		{10000, 10500, 5},
		{10000, 9000, -10},
		{10000, 10000, 0},
		{0, 0, 0},
		{0, 1, math.Inf(1)},
	}
	for _, tt := range tests {
		if got := DriftPct(tt.approved, tt.live); got != tt.want {
			t.Errorf("DriftPct(%d,%d) = %v, want %v", tt.approved, tt.live, got, tt.want)
		}
	}
}

func TestCheckDrift(t *testing.T) {
	tests := []struct {
		name           string
		approved, live domain.Cents
		wantOK         bool
		wantMsg        []string
	}{
		{"exactly 5pct is ok", 10000, 10500, true, nil},
		{"decrease ok", 10000, 8000, true, nil},
		{"over threshold", 10000, 10600, false, []string{"S$106.00", "6.00%", "S$100.00", "5%"}},
		{"from zero", 0, 100, false, []string{"unknown amount"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, r := CheckDrift(tt.approved, tt.live, cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if r != (domain.Reason{}) {
					t.Errorf("expected empty reason, got %+v", r)
				}
				return
			}
			if r.Code != domain.ReasonPriceDrift {
				t.Errorf("code = %s", r.Code)
			}
			for _, w := range tt.wantMsg {
				if !strings.Contains(r.Message, w) {
					t.Errorf("message %q missing %q", r.Message, w)
				}
			}
		})
	}
}

func TestSaturatingMath(t *testing.T) {
	max := domain.Cents(math.MaxInt64)
	if got := satAdd(max, 1); got != max {
		t.Errorf("satAdd overflow = %d", got)
	}
	if got := satAdd(1, 2); got != 3 {
		t.Errorf("satAdd = %d", got)
	}
	if got := satMul(max/2, 3); got != max {
		t.Errorf("satMul overflow = %d", got)
	}
	if got := satMul(7, 3); got != 21 {
		t.Errorf("satMul = %d", got)
	}
	if got := satMul(0, 3); got != 0 {
		t.Errorf("satMul zero = %d", got)
	}
}
