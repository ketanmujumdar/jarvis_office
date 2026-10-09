package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
)

type sseFrame struct {
	id, event, data string
}

// readFrames reads SSE frames from the stream into a channel until it closes.
func readFrames(t *testing.T, body *bufio.Reader) <-chan sseFrame {
	out := make(chan sseFrame, 32)
	go func() {
		defer close(out)
		var f sseFrame
		for {
			line, err := body.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if f.event != "" {
					out <- f
				}
				f = sseFrame{}
			case strings.HasPrefix(line, "id: "):
				f.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out
}

func openStream(t *testing.T, ts *httptest.Server, query string) (<-chan sseFrame, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/events"+query, nil)
	res, err := ts.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		cancel()
		t.Fatalf("status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	return readFrames(t, bufio.NewReader(res.Body)), func() { cancel(); res.Body.Close() }
}

func nextFrame(t *testing.T, ch <-chan sseFrame, skipHeartbeat bool) sseFrame {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatal("stream closed")
			}
			if skipHeartbeat && f.event == string(events.Heartbeat) {
				continue
			}
			return f
		case <-deadline:
			t.Fatal("timed out waiting for SSE frame")
		}
	}
}

// waitSubscribed publishes nothing until the handler has subscribed (heartbeat proves the loop runs).
func TestSSEPerRequestAndGlobal(t *testing.T) {
	h := newHarness(t)
	ts := httptest.NewServer(h.h)
	defer ts.Close()

	perReq, closeA := openStream(t, ts, "?request_id="+requestID+"&token="+managerID)
	defer closeA()
	global, closeB := openStream(t, ts, "?token="+approverID)
	defer closeB()
	// Heartbeats arrive on both streams, proving the subscriptions are live.
	if f := nextFrame(t, perReq, false); f.event != "heartbeat" || f.id != "" {
		t.Fatalf("first frame = %+v", f)
	}
	nextFrame(t, global, false)

	h.bus.Publish(events.RequestStatusChanged, missingID, map[string]string{"to": "searching"})
	h.bus.Publish(events.RequestStatusChanged, requestID, map[string]string{"to": "quoted"})

	f := nextFrame(t, perReq, true)
	if f.event != "request.status_changed" || f.id == "" {
		t.Fatalf("per-request frame = %+v", f)
	}
	var ev events.Event
	if err := json.Unmarshal([]byte(f.data), &ev); err != nil || ev.RequestID != requestID {
		t.Fatalf("data = %s (%v)", f.data, err)
	}
	// Global stream sees both, in order.
	if g := nextFrame(t, global, true); !strings.Contains(g.data, missingID) {
		t.Fatalf("global first = %+v", g)
	}
	if g := nextFrame(t, global, true); !strings.Contains(g.data, requestID) {
		t.Fatalf("global second = %+v", g)
	}
}

func TestSSEAuthAndValidation(t *testing.T) {
	h := newHarness(t)
	if r := h.do("GET", "/api/v1/events", "", nil); r.code != 401 {
		t.Fatalf("anonymous = %d", r.code)
	}
	if r := h.do("GET", "/api/v1/events?request_id=nope", managerID, nil); r.code != 400 {
		t.Fatalf("bad request_id = %d", r.code)
	}
}

func TestSSEUnsubscribesOnDisconnect(t *testing.T) {
	h := newHarness(t)
	ts := httptest.NewServer(h.h)
	defer ts.Close()
	ch, closeFn := openStream(t, ts, "?token="+managerID)
	nextFrame(t, ch, false)
	closeFn()
	// After disconnect the handler returns and cancels its subscription; publishing must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.bus.Publish(events.Heartbeat, "", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked after disconnect")
	}
}
