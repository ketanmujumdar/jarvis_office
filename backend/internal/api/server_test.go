package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		err  error
		code int
		name string
	}{
		{fmt.Errorf("x: %w", domain.ErrValidation), 400, "validation_failed"},
		{domain.ErrUnauthorized, 401, "unauthorized"},
		{domain.ErrForbidden, 403, "forbidden"},
		{fmt.Errorf("wrap: %w", domain.ErrNotFound), 404, "not_found"},
		{domain.ErrInvalidTransition, 409, "invalid_transition"},
		{domain.ErrNoActiveEnrollment, 409, "no_active_enrollment"},
		{domain.ErrConflict, 409, "conflict"},
		{domain.ErrUpstream, 502, "upstream_error"},
		{domain.ErrNotImplemented, 501, "not_implemented"},
		{errors.New("boom"), 500, "internal"},
	}
	for _, tt := range tests {
		got, code := StatusFor(tt.err)
		if got != tt.code || code != tt.name {
			t.Errorf("StatusFor(%v) = %d %s, want %d %s", tt.err, got, code, tt.code, tt.name)
		}
	}
}

// Every route in the canonical table has a real handler (no 501 stubs left).
func TestEveryRouteHasHandler(t *testing.T) {
	hs := (&Server{}).handlers()
	var missing, extra []string
	inRoutes := map[string]bool{}
	for _, r := range Routes {
		inRoutes[r] = true
		if hs[r] == nil {
			missing = append(missing, r)
		}
	}
	for r := range hs {
		if !inRoutes[r] {
			extra = append(extra, r)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Fatalf("missing handlers: %v; handlers not in Routes: %v", missing, extra)
	}
}

func TestHealthz(t *testing.T) {
	// No store: plain ok.
	rec := httptest.NewRecorder()
	NewServer(Deps{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}

	h := newHarness(t)
	if r := h.do("GET", "/healthz", "", nil); r.code != 200 || !strings.Contains(string(r.body), `"db":"ok"`) {
		t.Fatalf("healthz = %d %s", r.code, r.body)
	}
	h.st.pingErr = errors.New("down")
	if r := h.do("GET", "/healthz", "", nil); r.code != 503 {
		t.Fatalf("healthz with db down = %d", r.code)
	}
}

func TestNilDepsAnswer501(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.Orchestrator, d.Approvals, d.Agent, d.Tools, d.Realtime, d.Bus = nil, nil, nil, nil, nil, nil
	})
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/requests"},
		{"GET", "/api/v1/approvals"},
		{"POST", "/api/v1/agent/chat"},
		{"GET", "/api/v1/agent/tools"},
		{"POST", "/api/v1/realtime/session"},
		{"GET", "/api/v1/events"},
		{"POST", "/api/v1/enrollments"},
	} {
		if r := h.do(c.method, c.path, adminID, `{}`); r.code != 501 || r.errCode(t) != "not_implemented" {
			t.Errorf("%s %s = %d %s", c.method, c.path, r.code, r.body)
		}
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/v1/nope", managerID, nil)
	if r.code != 404 || r.errCode(t) != "not_found" {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}

func TestInvalidPathIDIs404(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/api/v1/requests/abc", "/api/v1/approvals/1", "/api/v1/admin/catalog/x"} {
		if r := h.do("GET", p, adminID, nil); r.code != 404 {
			t.Errorf("GET %s = %d", p, r.code)
		}
	}
}

func TestCORS(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		origin    string
		wantAllow bool
	}{
		{"http://localhost:5173", true},
		{"http://evil.example", false},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/requests", nil)
		req.Header.Set("Origin", tt.origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s preflight = %d", tt.origin, rec.Code)
		}
		got := rec.Header().Get("Access-Control-Allow-Origin") == tt.origin
		if got != tt.wantAllow {
			t.Errorf("%s allowed = %v, want %v", tt.origin, got, tt.wantAllow)
		}
		if tt.wantAllow && !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Errorf("allow headers = %q", rec.Header().Get("Access-Control-Allow-Headers"))
		}
	}
	// Wildcard reflects any origin.
	hw := newHarness(t, func(d *Deps) { d.Config.CORSOrigins = []string{"*"} })
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:61234")
	rec := httptest.NewRecorder()
	hw.h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:61234" {
		t.Errorf("wildcard CORS header = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestPanicRecovery(t *testing.T) {
	h := newHarness(t)
	h.srv.mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	r := h.do("GET", "/boom", "", nil)
	if r.code != 500 || r.errCode(t) != "internal" || strings.Contains(string(r.body), "kaboom") {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}

func TestPagination(t *testing.T) {
	h := newHarness(t)
	for q, want := range map[string]int{
		"": 200, "?limit=10&offset=5": 200, "?limit=0": 400, "?limit=201": 400, "?limit=x": 400, "?offset=-1": 400,
	} {
		if r := h.do("GET", "/api/v1/requests"+q, managerID, nil); r.code != want {
			t.Errorf("GET /requests%s = %d, want %d", q, r.code, want)
		}
	}
}
