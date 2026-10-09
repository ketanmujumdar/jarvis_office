package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
)

func defaultVerifyWebhook(header string, body []byte, secret string, now time.Time) error {
	return reap.VerifyWebhookSignature(header, body, secret, now)
}

// reapWebhook is the optional Reap webhook receiver (REAP_WEBHOOKS_ENABLED). It never trusts the
// payload for state: after verifying the signature it only uses data.id to find the payment or
// enrollment and then re-reads the resource from Reap through the orchestrator (the same path as
// polling). Deliveries are deduplicated on the envelope id.
func (s *Server) reapWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.d.Config.ReapWebhooksEnabled {
		writeError(w, fmt.Errorf("%w: webhooks are disabled (REAP_WEBHOOKS_ENABLED=false)", domain.ErrNotFound))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		writeJSON(w, http.StatusBadRequest, errBody("validation_failed", "unreadable body"))
		return
	}
	sig := r.Header.Get(reap.WebhookSignatureHeader)
	if sig == "" || s.d.VerifyWebhook(sig, body, s.d.Config.ReapWebhookSecret, s.d.Clock()) != nil {
		writeJSON(w, http.StatusBadRequest, errBody("validation_failed", "bad signature"))
		return
	}
	var ev reap.WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" {
		writeJSON(w, http.StatusBadRequest, errBody("validation_failed", "invalid event envelope"))
		return
	}
	if !s.webhook.add(ev.ID) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "duplicate"})
		return
	}
	var data struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ev.Data, &data)
	_ = s.d.Audit.Record(r.Context(), "", domain.ActorSystem, "reap", audit.ReapWebhookReceived,
		map[string]string{"event_id": ev.ID, "type": ev.Type, "resource_id": data.ID})

	// Reconcile in the background; Reap wants a fast 2xx.
	if data.ID != "" {
		go s.reconcile(data.ID, strings.ToUpper(ev.Type))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "accepted"})
}

func errBody(code, msg string) ErrorBody {
	var b ErrorBody
	b.Error.Code, b.Error.Message = code, msg
	return b
}

// reconcile maps a Reap resource id to our payment or enrollment and refreshes it from Reap.
func (s *Server) reconcile(resourceID, typ string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if s.d.Store == nil || s.d.Orchestrator == nil {
		return
	}
	if !strings.Contains(typ, "ENROLL") {
		if p, err := s.d.Store.Payments().GetByCheckoutID(ctx, resourceID); err == nil {
			if _, err := s.d.Orchestrator.RefreshPayments(ctx, p.RequestID); err != nil {
				s.log.Warn("webhook refresh payments", "request_id", p.RequestID, "err", err)
			}
			return
		}
	}
	if _, err := s.d.Store.Enrollments().GetByReapID(ctx, resourceID); err == nil {
		if _, err := s.d.Orchestrator.CurrentEnrollment(ctx); err != nil {
			s.log.Warn("webhook refresh enrollment", "err", err)
		}
	}
}

// dedupe remembers the last n event ids.
type dedupe struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	order []string
	n     int
}

func newDedupe(n int) *dedupe { return &dedupe{seen: map[string]struct{}{}, n: n} }

// add returns false if id was already seen.
func (d *dedupe) add(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[id]; ok {
		return false
	}
	d.seen[id] = struct{}{}
	d.order = append(d.order, id)
	if len(d.order) > d.n {
		delete(d.seen, d.order[0])
		d.order = d.order[1:]
	}
	return true
}
