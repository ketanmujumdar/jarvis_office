package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
)

// sse streams events.Bus events as text/event-stream. ?request_id= filters to one request; omit it
// for the global stream (approver console, dashboards). A heartbeat event is sent every
// HeartbeatInterval. Delivery is best-effort; clients re-fetch GET /requests/{id} on reconnect.
func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Bus != nil, "event bus"); err != nil {
		s.fail(w, r, err)
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if requestID != "" && !uuidRe.MatchString(requestID) {
		s.fail(w, r, validationf("request_id must be a UUID"))
		return
	}
	rc := http.NewResponseController(w)

	ch, cancel := s.d.Bus.Subscribe(requestID)
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Content-Encoding", "identity")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Tell EventSource to retry after 3 s and flush headers so the client sees the stream open.
	if _, err := fmt.Fprint(w, "retry: 3000\n: connected\n: "+strings.Repeat(" ", 2048)+"\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		s.log.Warn("sse flush unsupported", "err", err)
		return
	}
	// No write deadline for a long-lived stream (ignored if unsupported).
	_ = rc.SetWriteDeadline(time.Time{})

	tick := time.NewTicker(s.d.HeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := writeEvent(w, ev); err != nil {
				return
			}
		case <-tick.C:
			hb := events.Event{Type: events.Heartbeat, RequestID: requestID, At: s.d.Clock().UTC(), Data: map[string]any{}}
			if err := writeEvent(w, hb); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// writeEvent writes one SSE frame. Heartbeats carry id 0 and omit the `id:` line so they do not
// reset the client's Last-Event-ID.
func writeEvent(w http.ResponseWriter, ev events.Event) error {
	data, err := ev.MarshalData()
	if err != nil {
		return err
	}
	var b strings.Builder
	if ev.ID > 0 {
		fmt.Fprintf(&b, "id: %d\n", ev.ID)
	}
	fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", ev.Type, data)
	_, err = w.Write([]byte(b.String()))
	return err
}
