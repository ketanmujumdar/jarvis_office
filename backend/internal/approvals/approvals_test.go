package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

var (
	maya   = domain.User{ID: "u-maya", Name: "Maya", Role: domain.RoleManager}
	daniel = domain.User{ID: "u-daniel", Name: "Daniel", Role: domain.RoleApprover}
	priya  = domain.User{ID: "u-priya", Name: "Priya", Role: domain.RoleAdmin}

	policyReasons = []domain.Reason{{Code: domain.ReasonOffList, Message: "Standing desk is not on the approved catalog."}}
)

func strp(s string) *string               { return &s }
func centsp(c domain.Cents) *domain.Cents { return &c }

type recordingHandler struct {
	calls []domain.Approval
	err   error
}

func (h *recordingHandler) OnApprovalDecided(ctx context.Context, a domain.Approval) error {
	h.calls = append(h.calls, a)
	return h.err
}

// fixture: request r1 (quoted) by Maya with two lines; l1 on-list with 5 offers, l2 off-list with 1.
func fixture(status domain.RequestStatus) *fakeStore {
	fs := newFakeStore()
	fs.st.users[maya.ID] = maya
	fs.st.catalog["cat-paper"] = domain.CatalogItem{ID: "cat-paper", SKU: "a4", Name: "A4 Paper", Active: true}
	fs.st.requests["r1"] = domain.PurchaseRequest{ID: "r1", RequesterID: maya.ID, Status: status, TotalCents: 120000, Currency: "SGD"}
	fs.st.requests["r2"] = domain.PurchaseRequest{ID: "r2", RequesterID: "u-ghost", Status: domain.StatusQuoted}
	fs.st.lines = []domain.LineItem{
		{ID: "l2", RequestID: "r1", Position: 1, Description: "Standing desk", Qty: 1},
		{ID: "l1", RequestID: "r1", Position: 0, CatalogItemID: strp("cat-paper"), Description: "A4 paper", Qty: 10},
		{ID: "lx", RequestID: "r2", Position: 0, CatalogItemID: strp("cat-deleted"), Description: "Gone", Qty: 1},
	}
	fs.st.offers = []domain.Offer{
		{ID: "o5", LineItemID: "l1", Rank: 5, Available: true},
		{ID: "o2", LineItemID: "l1", Rank: 2, Available: true},
		{ID: "o4", LineItemID: "l1", Rank: 4, Available: true},
		{ID: "o1", LineItemID: "l1", Rank: 1, Available: true},
		{ID: "o3", LineItemID: "l1", Rank: 3, Available: true},
		{ID: "d1", LineItemID: "l2", Rank: 1, Available: true},
	}
	return fs
}

func newSvc(fs *fakeStore) (*Impl, *recordingHandler, *events.Memory) {
	h := &recordingHandler{}
	bus := events.NewMemory(64)
	s := New(Deps{Store: fs})
	s.SetHandler(h)
	s.SetEvents(bus)
	return s, h, bus
}

func drain(ch <-chan events.Event) []events.Type {
	var out []events.Type
	for {
		select {
		case e := <-ch:
			out = append(out, e.Type)
		default:
			return out
		}
	}
}

func auditTypes(fs *fakeStore) []string {
	out := []string{}
	for _, e := range fs.st.audit {
		out = append(out, e.Type)
	}
	return out
}

// ---------- Create ----------

func TestCreate(t *testing.T) {
	tests := []struct {
		name       string
		status     domain.RequestStatus
		in         CreateInput
		failOn     map[string]error
		wantErr    error
		wantStatus domain.RequestStatus // request status after the call
	}{
		{"policy approval from quoted", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, AmountCents: 120000},
			nil, nil, domain.StatusPendingApproval},
		{"price drift from checking_out", domain.StatusCheckingOut,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPriceDrift, Reasons: []domain.Reason{{Code: domain.ReasonPriceDrift, Message: "up 6%"}}, AmountCents: 10600, PrevCents: centsp(10000)},
			nil, nil, domain.StatusPendingApproval},
		{"zero amount allowed", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, nil, domain.StatusPendingApproval},
		{"invalid transition from approved", domain.StatusApproved,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, domain.ErrInvalidTransition, domain.StatusApproved},
		{"invalid transition from parsing", domain.StatusParsing,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, domain.ErrInvalidTransition, domain.StatusParsing},
		{"already pending_approval", domain.StatusPendingApproval,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, domain.ErrInvalidTransition, domain.StatusPendingApproval},
		{"missing request", domain.StatusQuoted,
			CreateInput{RequestID: "nope", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, domain.ErrNotFound, domain.StatusQuoted},
		{"empty request id", domain.StatusQuoted,
			CreateInput{RequestID: " ", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			nil, domain.ErrValidation, domain.StatusQuoted},
		{"unknown kind", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: "whim", Reasons: policyReasons},
			nil, domain.ErrValidation, domain.StatusQuoted},
		{"no reasons", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy},
			nil, domain.ErrValidation, domain.StatusQuoted},
		{"negative amount", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, AmountCents: -1},
			nil, domain.ErrValidation, domain.StatusQuoted},
		{"drift without prev", domain.StatusCheckingOut,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPriceDrift, Reasons: policyReasons, AmountCents: 1},
			nil, domain.ErrValidation, domain.StatusCheckingOut},
		{"drift with negative prev", domain.StatusCheckingOut,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPriceDrift, Reasons: policyReasons, PrevCents: centsp(-1)},
			nil, domain.ErrValidation, domain.StatusCheckingOut},
		{"policy with prev", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, PrevCents: centsp(1)},
			nil, domain.ErrValidation, domain.StatusQuoted},
		{"transition fails => rollback approval", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			map[string]error{"Requests.TransitionStatus": domain.ErrConflict}, domain.ErrConflict, domain.StatusQuoted},
		{"audit fails => rollback all", domain.StatusQuoted,
			CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons},
			map[string]error{"Audit.Append": errBoom}, errBoom, domain.StatusQuoted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := fixture(tt.status)
			for k, v := range tt.failOn {
				fs.failOn[k] = v
			}
			s, _, bus := newSvc(fs)
			ch, cancel := bus.Subscribe("")
			defer cancel()

			a, err := s.Create(context.Background(), tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if len(fs.st.approvals) != 0 {
					t.Errorf("approval persisted despite error: %+v", fs.st.approvals)
				}
				if len(fs.st.audit) != 0 {
					t.Errorf("audit persisted despite error: %v", auditTypes(fs))
				}
				if got := drain(ch); len(got) != 0 {
					t.Errorf("events published despite error: %v", got)
				}
				if r, ok := fs.st.requests["r1"]; ok && r.Status != tt.wantStatus {
					t.Errorf("request status = %s, want %s", r.Status, tt.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if a.ID == "" || a.Status != domain.ApprovalPending || a.Kind != tt.in.Kind || a.AmountCents != tt.in.AmountCents {
				t.Errorf("approval = %+v", a)
			}
			if !reflect.DeepEqual(a.Reasons, tt.in.Reasons) {
				t.Errorf("reasons = %v", a.Reasons)
			}
			if got := fs.st.requests["r1"].Status; got != tt.wantStatus {
				t.Errorf("request status = %s, want %s", got, tt.wantStatus)
			}
			if got, want := auditTypes(fs), []string{"approval.requested", "request.status_changed"}; !reflect.DeepEqual(got, want) {
				t.Errorf("audit = %v, want %v", got, want)
			}
			for _, e := range fs.st.audit {
				if e.RequestID == nil || *e.RequestID != "r1" || e.ActorType != domain.ActorSystem || !json.Valid(e.Payload) {
					t.Errorf("bad audit event %+v", e)
				}
			}
			if got, want := drain(ch), []events.Type{events.RequestStatusChanged, events.ApprovalRequested}; !reflect.DeepEqual(got, want) {
				t.Errorf("events = %v, want %v", got, want)
			}
		})
	}
}

func TestCreateSecondPendingConflicts(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	ctx := context.Background()
	if _, err := s.Create(ctx, CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons}); err != nil {
		t.Fatal(err)
	}
	// Force the request back to a state that may open an approval, keeping the pending one.
	r := fs.st.requests["r1"]
	r.Status = domain.StatusCheckingOut
	fs.st.requests["r1"] = r
	_, err := s.Create(ctx, CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPriceDrift, Reasons: policyReasons, PrevCents: centsp(1)})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(fs.st.approvals) != 1 {
		t.Fatalf("approvals = %d, want 1", len(fs.st.approvals))
	}
}

func TestCreateNilReasonsSliceNeverPersisted(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	a, err := s.Create(context.Background(), CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	if a.Reasons == nil {
		t.Fatal("reasons nil")
	}
}

// ---------- Approve / Reject ----------

func TestDecide(t *testing.T) {
	type op int
	const (
		approve op = iota
		reject
	)
	tests := []struct {
		name        string
		op          op
		user        domain.User
		id          string
		prep        func(fs *fakeStore, id string)
		handlerErr  error
		failOn      map[string]error
		wantErr     error
		wantStatus  domain.ApprovalStatus // persisted status afterwards
		wantHandler int
	}{
		{"approver approves", approve, daniel, "", nil, nil, nil, nil, domain.ApprovalApproved, 1},
		{"admin approves", approve, priya, "", nil, nil, nil, nil, domain.ApprovalApproved, 1},
		{"approver rejects", reject, daniel, "", nil, nil, nil, nil, domain.ApprovalRejected, 1},
		{"manager forbidden to approve", approve, maya, "", nil, nil, nil, domain.ErrForbidden, domain.ApprovalPending, 0},
		{"manager forbidden to reject", reject, maya, "", nil, nil, nil, domain.ErrForbidden, domain.ApprovalPending, 0},
		{"empty role forbidden", approve, domain.User{ID: "x"}, "", nil, nil, nil, domain.ErrForbidden, domain.ApprovalPending, 0},
		{"approver without id", approve, domain.User{Role: domain.RoleApprover}, "", nil, nil, nil, domain.ErrValidation, domain.ApprovalPending, 0},
		{"unknown approval", approve, daniel, "missing", nil, nil, nil, domain.ErrNotFound, domain.ApprovalPending, 0},
		{"already approved", approve, daniel, "", func(fs *fakeStore, id string) {
			fs.st.approvals[0].Status = domain.ApprovalApproved
		}, nil, nil, domain.ErrConflict, domain.ApprovalApproved, 0},
		{"already rejected cannot be approved", approve, daniel, "", func(fs *fakeStore, id string) {
			fs.st.approvals[0].Status = domain.ApprovalRejected
		}, nil, nil, domain.ErrConflict, domain.ApprovalRejected, 0},
		{"request cancelled meanwhile", approve, daniel, "", func(fs *fakeStore, id string) {
			r := fs.st.requests["r1"]
			r.Status = domain.StatusCancelled
			fs.st.requests["r1"] = r
		}, nil, nil, domain.ErrConflict, domain.ApprovalPending, 0},
		{"lost race in Decide", approve, daniel, "", nil, nil, map[string]error{"Approvals.Decide": domain.ErrConflict},
			domain.ErrConflict, domain.ApprovalPending, 0},
		{"audit failure rolls back decision", reject, daniel, "", nil, nil, map[string]error{"Audit.Append": errBoom},
			errBoom, domain.ApprovalPending, 0},
		{"handler error surfaces but decision is kept", approve, daniel, "", nil, errBoom, nil,
			errBoom, domain.ApprovalApproved, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := fixture(domain.StatusQuoted)
			s, h, bus := newSvc(fs)
			ctx := context.Background()
			created, err := s.Create(ctx, CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, AmountCents: 120000})
			if err != nil {
				t.Fatal(err)
			}
			id := created.ID
			if tt.id != "" {
				id = tt.id
			}
			if tt.prep != nil {
				tt.prep(fs, id)
			}
			for k, v := range tt.failOn {
				fs.failOn[k] = v
			}
			h.err = tt.handlerErr
			auditBefore := len(fs.st.audit)
			ch, cancel := bus.Subscribe("r1")
			defer cancel()

			var a domain.Approval
			if tt.op == approve {
				a, err = s.Approve(ctx, id, tt.user, "  looks fine  ")
			} else {
				a, err = s.Reject(ctx, id, tt.user, "  too pricey  ")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got := fs.st.approvals[0].Status; got != tt.wantStatus {
				t.Errorf("persisted status = %s, want %s", got, tt.wantStatus)
			}
			if len(h.calls) != tt.wantHandler {
				t.Errorf("handler calls = %d, want %d", len(h.calls), tt.wantHandler)
			}
			decided := tt.wantStatus != domain.ApprovalPending && tt.wantHandler > 0
			if !decided {
				if len(fs.st.audit) != auditBefore {
					t.Errorf("audit written on failure: %v", auditTypes(fs)[auditBefore:])
				}
				if got := drain(ch); len(got) != 0 {
					t.Errorf("events on failure: %v", got)
				}
				return
			}
			// Decided path (including handler error): approval returned, audited, published.
			if a.Status != tt.wantStatus || a.ApproverID == nil || *a.ApproverID != tt.user.ID || a.DecidedAt == nil {
				t.Errorf("approval = %+v", a)
			}
			wantComment := "looks fine"
			if tt.op == reject {
				wantComment = "too pricey"
			}
			if a.Comment != wantComment {
				t.Errorf("comment = %q, want %q", a.Comment, wantComment)
			}
			if !reflect.DeepEqual(h.calls[0], a) {
				t.Errorf("handler got %+v, want %+v", h.calls[0], a)
			}
			last := fs.st.audit[len(fs.st.audit)-1]
			if last.Type != "approval.decided" || last.ActorType != domain.ActorUser || last.ActorID != tt.user.ID {
				t.Errorf("audit = %+v", last)
			}
			if got := drain(ch); !reflect.DeepEqual(got, []events.Type{events.ApprovalDecided}) {
				t.Errorf("events = %v", got)
			}
			// Approving again is a conflict and does not call the handler again (no double spend).
			if _, err := s.Approve(ctx, id, tt.user, ""); !errors.Is(err, domain.ErrConflict) {
				t.Errorf("second decision err = %v, want ErrConflict", err)
			}
			if len(h.calls) != tt.wantHandler {
				t.Errorf("handler called again")
			}
		})
	}
}

func TestDecideWithoutHandler(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s := New(Deps{Store: fs}) // no handler, no events
	a, err := s.Create(context.Background(), CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(context.Background(), a.ID, daniel, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNoExpiry(t *testing.T) {
	// MVP decision: approvals never expire. A pending approval stays decidable regardless of age,
	// and the Approval model carries no expiry field.
	if _, ok := reflect.TypeOf(domain.Approval{}).FieldByName("ExpiresAt"); ok {
		t.Fatal("Approval must not have ExpiresAt")
	}
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	a, err := s.Create(context.Background(), CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	fs.st.approvals[0].CreatedAt = fs.st.approvals[0].CreatedAt.AddDate(-1, 0, 0)
	if _, err := s.Approve(context.Background(), a.ID, daniel, ""); err != nil {
		t.Fatalf("year-old approval not decidable: %v", err)
	}
}

// ---------- Get / List ----------

func TestGetView(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	a, err := s.Create(context.Background(), CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, AmountCents: 120000})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Approval.ID != a.ID || v.Request.ID != "r1" || v.Request.Status != domain.StatusPendingApproval {
		t.Errorf("view header = %+v / %+v", v.Approval, v.Request)
	}
	if v.Requester.Name != "Maya" {
		t.Errorf("requester = %+v", v.Requester)
	}
	if len(v.Lines) != 2 || v.Lines[0].LineItem.ID != "l1" || v.Lines[1].LineItem.ID != "l2" {
		t.Fatalf("lines = %+v", v.Lines)
	}
	ids := func(os []domain.Offer) []string {
		out := []string{}
		for _, o := range os {
			out = append(out, o.ID)
		}
		return out
	}
	if got := ids(v.Lines[0].TopOffers); !reflect.DeepEqual(got, []string{"o1", "o2", "o3"}) {
		t.Errorf("top offers l1 = %v", got)
	}
	if got := ids(v.Lines[1].TopOffers); !reflect.DeepEqual(got, []string{"d1"}) {
		t.Errorf("top offers l2 = %v", got)
	}
	if v.Lines[0].Catalog == nil || v.Lines[0].Catalog.Name != "A4 Paper" {
		t.Errorf("catalog l1 = %+v", v.Lines[0].Catalog)
	}
	if v.Lines[1].Catalog != nil {
		t.Errorf("off-list line has catalog %+v", v.Lines[1].Catalog)
	}
	// JSON shape matches openapi ApprovalView (required keys present, top_offers never null).
	raw, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"approval", "request", "requester", "lines"} {
		if _, ok := m[k]; !ok {
			t.Errorf("json missing %q", k)
		}
	}
	line := m["lines"].([]any)[1].(map[string]any)
	if line["top_offers"] == nil {
		t.Error("top_offers is null")
	}
	if _, ok := line["catalog_item"]; ok {
		t.Error("catalog_item should be omitted for off-list")
	}
}

func TestGetViewTolerance(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	// r2: requester missing, catalog item deleted, no offers.
	a, err := s.Create(context.Background(), CreateInput{RequestID: "r2", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Requester.ID != "u-ghost" {
		t.Errorf("requester = %+v", v.Requester)
	}
	if len(v.Lines) != 1 || v.Lines[0].Catalog != nil || v.Lines[0].TopOffers == nil || len(v.Lines[0].TopOffers) != 0 {
		t.Errorf("lines = %+v", v.Lines)
	}
	if v.Lines[0].LineItem.Reasons == nil {
		t.Error("line reasons nil")
	}
}

func TestGetErrors(t *testing.T) {
	tests := []struct {
		name    string
		failOn  map[string]error
		id      string
		wantErr error
	}{
		{"not found", nil, "missing", domain.ErrNotFound},
		{"user repo error", map[string]error{"Users.Get": errBoom}, "", errBoom},
		{"catalog repo error", map[string]error{"Catalog.Get": errBoom}, "", errBoom},
		{"offer repo error", map[string]error{"Offers.ListByLineItem": errBoom}, "", errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := fixture(domain.StatusQuoted)
			s, _, _ := newSvc(fs)
			a, err := s.Create(context.Background(), CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tt.failOn {
				fs.failOn[k] = v
			}
			id := a.ID
			if tt.id != "" {
				id = tt.id
			}
			if _, err := s.Get(context.Background(), id); !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if _, err := s.List(context.Background(), store.ApprovalFilter{}); tt.failOn != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("list err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestList(t *testing.T) {
	fs := fixture(domain.StatusQuoted)
	s, _, _ := newSvc(fs)
	ctx := context.Background()
	a1, err := s.Create(ctx, CreateInput{RequestID: "r1", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.Create(ctx, CreateInput{RequestID: "r2", Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reject(ctx, a1.ID, daniel, "no"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		status domain.ApprovalStatus
		want   []string
	}{
		{"", []string{a2.ID, a1.ID}},
		{domain.ApprovalPending, []string{a2.ID}},
		{domain.ApprovalRejected, []string{a1.ID}},
		{domain.ApprovalApproved, []string{}},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			vs, err := s.List(ctx, store.ApprovalFilter{Status: tt.status})
			if err != nil {
				t.Fatal(err)
			}
			if vs == nil {
				t.Fatal("nil slice")
			}
			got := []string{}
			for _, v := range vs {
				got = append(got, v.Approval.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

// ---------- TopOffers ----------

func TestTopOffers(t *testing.T) {
	o := func(id string, rank int, avail bool) domain.Offer {
		return domain.Offer{ID: id, Rank: rank, Available: avail}
	}
	tests := []struct {
		name string
		in   []domain.Offer
		n    int
		want []string
	}{
		{"nil", nil, 3, []string{}},
		{"fewer than n", []domain.Offer{o("b", 2, true), o("a", 1, true)}, 3, []string{"a", "b"}},
		{"exactly n", []domain.Offer{o("c", 3, true), o("a", 1, true), o("b", 2, true)}, 3, []string{"a", "b", "c"}},
		{"more than n", []domain.Offer{o("d", 4, true), o("c", 3, true), o("a", 1, true), o("b", 2, true)}, 3, []string{"a", "b", "c"}},
		{"unranked last", []domain.Offer{o("z", 0, true), o("a", 1, true)}, 3, []string{"a", "z"}},
		{"unavailable after available", []domain.Offer{o("x", 1, false), o("b", 2, true)}, 3, []string{"b", "x"}},
		{"ties keep input order", []domain.Offer{o("p", 1, true), o("q", 1, true)}, 3, []string{"p", "q"}},
		{"n zero", []domain.Offer{o("a", 1, true)}, 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := append([]domain.Offer(nil), tt.in...)
			got := TopOffers(tt.in, tt.n)
			if got == nil {
				t.Fatal("nil result")
			}
			ids := []string{}
			for _, x := range got {
				ids = append(ids, x.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("got %v want %v", ids, tt.want)
			}
			if !reflect.DeepEqual(tt.in, in) {
				t.Error("input mutated")
			}
		})
	}
}
