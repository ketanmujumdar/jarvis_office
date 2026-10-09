package api

import (
	"net/http"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/orchestrator"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// CreateRequestBody is POST /requests.
type CreateRequestBody struct {
	Utterance string `json:"utterance"`
	Items     []struct {
		Description string         `json:"description"`
		Qty         *int           `json:"qty"`
		Urgency     domain.Urgency `json:"urgency"`
	} `json:"items"`
}

func (b CreateRequestBody) toInput(userID string) (orchestrator.CreateRequestInput, error) {
	in := orchestrator.CreateRequestInput{RequesterID: userID, Utterance: strings.TrimSpace(b.Utterance)}
	if in.Utterance == "" {
		return in, validationf("utterance is required")
	}
	for i, it := range b.Items {
		d := strings.TrimSpace(it.Description)
		if d == "" {
			return in, validationf("items[%d].description is required", i)
		}
		if it.Qty != nil && *it.Qty < 1 {
			return in, validationf("items[%d].qty must be at least 1", i)
		}
		u := it.Urgency
		if u == "" {
			u = domain.UrgencyNormal
		}
		if u != domain.UrgencyNormal && u != domain.UrgencyUrgent {
			return in, validationf("items[%d].urgency must be normal or urgent", i)
		}
		in.Items = append(in.Items, agents.RequestedItem{Description: d, Qty: it.Qty, Urgency: u})
	}
	return in, nil
}

func (s *Server) orch(w http.ResponseWriter, r *http.Request) bool {
	if err := need(s.d.Orchestrator != nil, "orchestrator"); err != nil {
		s.fail(w, r, err)
		return false
	}
	return true
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	var body CreateRequestBody
	if err := decode(r, &body, false); err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := body.toInput(currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pr, err := s.d.Orchestrator.CreateRequest(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, pr)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f := store.RequestFilter{Limit: limit, Offset: offset}
	if v := r.URL.Query().Get("status"); v != "" {
		for _, p := range strings.Split(v, ",") {
			st := domain.RequestStatus(strings.TrimSpace(p))
			if st == "" {
				continue
			}
			if !st.Valid() {
				s.fail(w, r, validationf("unknown status %q", st))
				return
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	switch strings.ToLower(r.URL.Query().Get("mine")) {
	case "", "false", "0":
	case "true", "1":
		f.RequesterID = currentUser(r).ID
	default:
		s.fail(w, r, validationf("mine must be true or false"))
		return
	}
	rs, err := s.d.Orchestrator.ListRequests(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if rs == nil {
		rs = []domain.PurchaseRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": rs})
}

// normalizeDetail guarantees non-null arrays in the JSON (the schema marks them required).
func normalizeDetail(d domain.RequestDetail) domain.RequestDetail {
	if d.LineItems == nil {
		d.LineItems = []domain.LineItem{}
	}
	for i := range d.LineItems {
		if d.LineItems[i].Reasons == nil {
			d.LineItems[i].Reasons = []domain.Reason{}
		}
	}
	if d.Offers == nil {
		d.Offers = []domain.Offer{}
	}
	if d.Approvals == nil {
		d.Approvals = []domain.Approval{}
	}
	for i := range d.Approvals {
		if d.Approvals[i].Reasons == nil {
			d.Approvals[i].Reasons = []domain.Reason{}
		}
	}
	if d.Payments == nil {
		d.Payments = []domain.Payment{}
	}
	return d
}

func (s *Server) writeDetail(w http.ResponseWriter, r *http.Request, d domain.RequestDetail, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, normalizeDetail(d))
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	d, err := s.d.Orchestrator.GetRequest(r.Context(), r.PathValue("id"))
	s.writeDetail(w, r, d, err)
}

func (s *Server) confirmRequest(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	var body struct {
		AddressID string `json:"address_id"`
	}
	if err := decode(r, &body, false); err != nil {
		s.fail(w, r, err)
		return
	}
	body.AddressID = strings.TrimSpace(body.AddressID)
	if body.AddressID == "" {
		s.fail(w, r, validationf("address_id is required: ask which delivery address to use"))
		return
	}
	if !uuidRe.MatchString(body.AddressID) {
		s.fail(w, r, validationf("address_id must be a UUID"))
		return
	}
	d, err := s.d.Orchestrator.Confirm(r.Context(), r.PathValue("id"),
		orchestrator.ConfirmInput{UserID: currentUser(r).ID, AddressID: body.AddressID})
	s.writeDetail(w, r, d, err)
}

func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	d, err := s.d.Orchestrator.Cancel(r.Context(), r.PathValue("id"), currentUser(r).ID)
	s.writeDetail(w, r, d, err)
}

// CheckoutStatus is GET /requests/{id}/checkout.
type CheckoutStatus struct {
	RequestID string               `json:"request_id"`
	Status    domain.RequestStatus `json:"status"`
	Payments  []domain.Payment     `json:"payments"`
}

func checkoutOf(d domain.RequestDetail) CheckoutStatus {
	d = normalizeDetail(d)
	return CheckoutStatus{RequestID: d.Request.ID, Status: d.Request.Status, Payments: d.Payments}
}

// checkoutStatus reads from the DB only (cheap to poll).
func (s *Server) checkoutStatus(w http.ResponseWriter, r *http.Request) {
	var (
		d   domain.RequestDetail
		err error
	)
	switch {
	case s.d.Store != nil:
		d, err = s.d.Store.Requests().Detail(r.Context(), r.PathValue("id"))
	case s.d.Orchestrator != nil:
		d, err = s.d.Orchestrator.GetRequest(r.Context(), r.PathValue("id"))
	default:
		err = need(false, "store")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutOf(d))
}

func (s *Server) refreshCheckout(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	d, err := s.d.Orchestrator.RefreshPayments(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutOf(d))
}

func (s *Server) requestAudit(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Store != nil, "store"); err != nil {
		s.fail(w, r, err)
		return
	}
	id := r.PathValue("id")
	if _, err := s.d.Store.Requests().Get(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	evs, err := s.d.Store.Audit().ListByRequest(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": nonNilAudit(evs)})
}

func nonNilAudit(evs []domain.AuditEvent) []domain.AuditEvent {
	if evs == nil {
		return []domain.AuditEvent{}
	}
	return evs
}
