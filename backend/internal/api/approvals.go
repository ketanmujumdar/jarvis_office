package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

func (s *Server) appr(w http.ResponseWriter, r *http.Request) bool {
	if err := need(s.d.Approvals != nil, "approvals service"); err != nil {
		s.fail(w, r, err)
		return false
	}
	return true
}

func normalizeView(v approvals.ApprovalView) approvals.ApprovalView {
	if v.Approval.Reasons == nil {
		v.Approval.Reasons = []domain.Reason{}
	}
	if v.Lines == nil {
		v.Lines = []approvals.LineView{}
	}
	for i := range v.Lines {
		if v.Lines[i].TopOffers == nil {
			v.Lines[i].TopOffers = []domain.Offer{}
		}
		if v.Lines[i].LineItem.Reasons == nil {
			v.Lines[i].LineItem.Reasons = []domain.Reason{}
		}
		if len(v.Lines[i].TopOffers) > 3 {
			v.Lines[i].TopOffers = v.Lines[i].TopOffers[:3]
		}
	}
	return v
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.appr(w, r) {
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f := store.ApprovalFilter{Limit: limit, Offset: offset}
	switch st := domain.ApprovalStatus(strings.TrimSpace(r.URL.Query().Get("status"))); st {
	case "", domain.ApprovalPending, domain.ApprovalApproved, domain.ApprovalRejected:
		f.Status = st
	default:
		s.fail(w, r, validationf("status must be pending, approved or rejected"))
		return
	}
	vs, err := s.d.Approvals.List(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]approvals.ApprovalView, 0, len(vs))
	for _, v := range vs {
		out = append(out, normalizeView(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": out})
}

func (s *Server) getApproval(w http.ResponseWriter, r *http.Request) {
	if !s.appr(w, r) {
		return
	}
	v, err := s.d.Approvals.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, normalizeView(v))
}

type decisionBody struct {
	Comment string `json:"comment"`
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	if !s.appr(w, r) {
		return
	}
	var body decisionBody
	if err := decode(r, &body, true); err != nil {
		s.fail(w, r, err)
		return
	}
	comment := strings.TrimSpace(body.Comment)
	if len(comment) > 2000 {
		s.fail(w, r, validationf("comment is too long (max 2000 characters)"))
		return
	}
	var (
		a   domain.Approval
		err error
	)
	if approve {
		a, err = s.d.Approvals.Approve(r.Context(), r.PathValue("id"), currentUser(r), comment)
	} else {
		a, err = s.d.Approvals.Reject(r.Context(), r.PathValue("id"), currentUser(r), comment)
	}
	if err != nil {
		// The decision is committed before the orchestrator continues the flow. If only that
		// follow-up failed, the approval comes back decided: report success, log the follow-up error
		// (the request itself shows the failure).
		if a.ID != "" && a.Status != "" && a.Status != domain.ApprovalPending && !isDecisionRejection(err) {
			s.log.Warn("approval decided but follow-up failed", "approval_id", a.ID, "err", err)
		} else {
			s.fail(w, r, err)
			return
		}
	}
	if a.Reasons == nil {
		a.Reasons = []domain.Reason{}
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) { s.decide(w, r, true) }
func (s *Server) reject(w http.ResponseWriter, r *http.Request)  { s.decide(w, r, false) }

// isDecisionRejection reports errors meaning the decision itself was refused (nothing committed).
func isDecisionRejection(err error) bool {
	for _, s := range []error{domain.ErrConflict, domain.ErrForbidden, domain.ErrNotFound, domain.ErrValidation,
		domain.ErrInvalidTransition, domain.ErrUnauthorized} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}
