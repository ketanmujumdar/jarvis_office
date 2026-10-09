package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SSEEvent is one parsed server-sent event. Data is the raw `data:` payload, which for the Jarvis
// api is the JSON of events.Event {id, type, request_id, at, data}.
type SSEEvent struct {
	ID    string
	Event string
	Data  json.RawMessage
}

// Envelope decodes the Jarvis event envelope from Data.
func (e SSEEvent) Envelope() (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(e.Data, &env)
	return env, err
}

// Envelope is events.Event as seen on the wire.
type Envelope struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	At        time.Time       `json:"at"`
	Data      json.RawMessage `json:"data"`
}

// ParseSSE reads an event stream and calls fn for every dispatched event until r ends or fn
// returns false. It follows the WHATWG rules the api relies on: `field: value` lines, multiple
// data lines joined by "\n", comments (":") ignored, blank line dispatches.
func ParseSSE(r io.Reader, fn func(SSEEvent) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var cur SSEEvent
	var data []string
	has := false
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			if has {
				cur.Data = json.RawMessage(strings.Join(data, "\n"))
				if cur.Event == "" {
					cur.Event = "message"
				}
				if !fn(cur) {
					return nil
				}
			}
			cur, data, has = SSEEvent{}, nil, false
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			cur.ID, has = value, true
		case "event":
			cur.Event, has = value, true
		case "data":
			data, has = append(data, value), true
		}
	}
	return sc.Err()
}

// Stream is a live SSE subscription that buffers every event it receives.
type Stream struct {
	mu     sync.Mutex
	events []SSEEvent
	notify chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Subscribe opens GET {base}/api/v1/events?token=..&request_id=.. and waits until the response
// headers arrive (so events published afterwards are not missed).
func Subscribe(ctx context.Context, base, token, requestID string) (*Stream, error) {
	q := url.Values{}
	if token != "" {
		q.Set("token", token)
	}
	if requestID != "" {
		q.Set("request_id", requestID)
	}
	ctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/events?"+q.Encode(), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		cancel()
		return nil, errf("sse: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		cancel()
		return nil, errf("sse: content-type %q, want text/event-stream", ct)
	}
	s := &Stream{notify: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer resp.Body.Close()
		err := ParseSSE(resp.Body, func(e SSEEvent) bool {
			s.mu.Lock()
			s.events = append(s.events, e)
			s.mu.Unlock()
			select {
			case s.notify <- struct{}{}:
			default:
			}
			return true
		})
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
	}()
	return s, nil
}

// Close stops the subscription.
func (s *Stream) Close() {
	s.cancel()
	<-s.done
}

// Events returns a snapshot of everything received so far.
func (s *Stream) Events() []SSEEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SSEEvent(nil), s.events...)
}

// ForRequest returns events (excluding heartbeats) whose envelope request_id matches.
func (s *Stream) ForRequest(requestID string) []Envelope {
	var out []Envelope
	for _, e := range s.Events() {
		if e.Event == "heartbeat" {
			continue
		}
		env, err := e.Envelope()
		if err != nil || env.RequestID != requestID {
			continue
		}
		if env.Type == "" {
			env.Type = e.Event
		}
		out = append(out, env)
	}
	return out
}

// WaitFor blocks until pred holds for the received events or the timeout passes.
func (s *Stream) WaitFor(timeout time.Duration, pred func([]SSEEvent) bool) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if pred(s.Events()) {
			return true
		}
		select {
		case <-s.notify:
		case <-s.done:
			return pred(s.Events())
		case <-deadline.C:
			return false
		}
	}
}

// Seq returns the event types for a request, with status changes rendered as "status:<to>".
func Seq(envs []Envelope) []string {
	out := make([]string, 0, len(envs))
	for _, e := range envs {
		if e.Type == "request.status_changed" {
			var d struct {
				To string `json:"to"`
			}
			_ = json.Unmarshal(e.Data, &d)
			out = append(out, "status:"+d.To)
			continue
		}
		out = append(out, e.Type)
	}
	return out
}

// IsSubsequence reports whether want appears in got in order (not necessarily adjacent), and if
// not, the index of the first missing element.
func IsSubsequence(got, want []string) (bool, int) {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want), i
}
