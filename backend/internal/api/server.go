// Package api is the HTTP layer: REST + SSE per docs/openapi.yaml. Owner: agent F.
//
// Conventions:
//   - JSON everywhere; errors are {"error": {"code": "...", "message": "..."}} (see writeError).
//   - Fake auth: POST /api/v1/auth/login {email} (or {name, role}) returns {token, user} and sets
//     the jarvis_token cookie. Clients send "Authorization: Bearer <token>", the cookie, or
//     ?token= (SSE). The token is the user id. No passwords, no real security (demo only).
//   - Role checks: creating/confirming/cancelling requests needs manager|admin; approvals need
//     approver|admin; /api/v1/admin/* needs admin.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/allowlist"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/orchestrator"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Deps are everything the handlers need. Nil collaborators make their routes answer 501.
type Deps struct {
	Config       config.Config
	Store        store.Store
	Orchestrator orchestrator.Service
	Approvals    approvals.Service
	Agent        agents.TextAgent
	Tools        agents.ToolExecutor
	Realtime     realtime.Minter
	Bus          events.Bus
	Audit        audit.Logger

	// Optional.
	Logger            *slog.Logger     // default: discard
	Clock             func() time.Time // default: time.Now
	HeartbeatInterval time.Duration    // SSE heartbeat, default 15s
	// AllowedMerchants is the merchant allow-list; vendors outside it cannot be marked allowed.
	// Default: allowlist.Default() (backend/seed/allowed_merchants.tsv).
	AllowedMerchants *allowlist.Set
	// VerifyWebhook checks the X-Reap-Webhook-Signature header; default reap.VerifyWebhookSignature.
	VerifyWebhook func(header string, body []byte, secret string, now time.Time) error
}

// Server serves the API.
type Server struct {
	d       Deps
	mux     *http.ServeMux
	log     *slog.Logger
	webhook *dedupe
}

// NewServer registers all routes.
func NewServer(d Deps) *Server {
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.HeartbeatInterval <= 0 {
		d.HeartbeatInterval = 15 * time.Second
	}
	if d.Audit == nil {
		d.Audit = audit.Nop{}
	}
	if d.AllowedMerchants == nil {
		def := allowlist.Default()
		d.AllowedMerchants = &def
	}
	if d.VerifyWebhook == nil {
		d.VerifyWebhook = defaultVerifyWebhook
	}
	s := &Server{d: d, mux: http.NewServeMux(), log: d.Logger, webhook: newDedupe(1024)}
	s.routes()
	return s
}

// Handler returns the root handler with recovery, logging and CORS middleware.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.logRequests(s.cors(s.mux)))
}

// Routes is the canonical route table (method + Go 1.22 pattern). Keep in sync with openapi.yaml.
var Routes = []string{
	"GET /healthz",

	"POST /api/v1/auth/login",
	"GET /api/v1/auth/me",
	"GET /api/v1/auth/users",

	"POST /api/v1/requests",
	"GET /api/v1/requests",
	"GET /api/v1/requests/{id}",
	"POST /api/v1/requests/{id}/confirm",
	"POST /api/v1/requests/{id}/cancel",
	"GET /api/v1/requests/{id}/checkout",
	"POST /api/v1/requests/{id}/checkout/refresh",
	"GET /api/v1/requests/{id}/audit",

	"GET /api/v1/events",

	"GET /api/v1/approvals",
	"GET /api/v1/approvals/{id}",
	"POST /api/v1/approvals/{id}/approve",
	"POST /api/v1/approvals/{id}/reject",

	"GET /api/v1/orders",
	"GET /api/v1/spend/monthly",
	"GET /api/v1/audit",

	"GET /api/v1/catalog",
	"GET /api/v1/addresses",

	"POST /api/v1/agent/chat",
	"GET /api/v1/agent/sessions/{id}/messages",
	"GET /api/v1/agent/tools",
	"POST /api/v1/agent/tools/{name}",
	"POST /api/v1/realtime/session",

	"POST /api/v1/enrollments",
	"GET /api/v1/enrollments/current",

	"GET /api/v1/admin/catalog",
	"POST /api/v1/admin/catalog",
	"GET /api/v1/admin/catalog/{id}",
	"PUT /api/v1/admin/catalog/{id}",
	"DELETE /api/v1/admin/catalog/{id}",
	"GET /api/v1/admin/vendors",
	"POST /api/v1/admin/vendors",
	"GET /api/v1/admin/vendors/{id}",
	"PUT /api/v1/admin/vendors/{id}",
	"DELETE /api/v1/admin/vendors/{id}",
	"GET /api/v1/admin/policy",
	"PUT /api/v1/admin/policy",
	"GET /api/v1/admin/addresses",
	"POST /api/v1/admin/addresses",
	"GET /api/v1/admin/addresses/{id}",
	"PUT /api/v1/admin/addresses/{id}",
	"DELETE /api/v1/admin/addresses/{id}",
	"GET /api/v1/admin/system-prompt",
	"PUT /api/v1/admin/system-prompt",
	"GET /api/v1/admin/system-prompt/default",

	"POST /api/v1/webhooks/reap",
}

// handlers maps every entry of Routes to its handler. A test asserts the two stay in sync.
func (s *Server) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /healthz": s.healthz,

		"POST /api/v1/auth/login": s.login,
		"GET /api/v1/auth/me":     s.authed(s.me),
		"GET /api/v1/auth/users":  s.listUsers,

		"POST /api/v1/requests":                       s.authed(s.roles(s.createRequest, domain.RoleManager, domain.RoleAdmin)),
		"GET /api/v1/requests":                        s.authed(s.listRequests),
		"GET /api/v1/requests/{id}":                   s.authed(s.getRequest),
		"POST /api/v1/requests/{id}/confirm":          s.authed(s.roles(s.confirmRequest, domain.RoleManager, domain.RoleAdmin)),
		"POST /api/v1/requests/{id}/cancel":           s.authed(s.roles(s.cancelRequest, domain.RoleManager, domain.RoleAdmin)),
		"GET /api/v1/requests/{id}/checkout":          s.authed(s.checkoutStatus),
		"POST /api/v1/requests/{id}/checkout/refresh": s.authed(s.refreshCheckout),
		"GET /api/v1/requests/{id}/audit":             s.authed(s.requestAudit),

		"GET /api/v1/events": s.authed(s.sse),

		"GET /api/v1/approvals":               s.authed(s.roles(s.listApprovals, domain.RoleApprover, domain.RoleAdmin)),
		"GET /api/v1/approvals/{id}":          s.authed(s.getApproval),
		"POST /api/v1/approvals/{id}/approve": s.authed(s.roles(s.approve, domain.RoleApprover, domain.RoleAdmin)),
		"POST /api/v1/approvals/{id}/reject":  s.authed(s.roles(s.reject, domain.RoleApprover, domain.RoleAdmin)),

		"GET /api/v1/orders":        s.authed(s.listOrders),
		"GET /api/v1/spend/monthly": s.authed(s.monthlySpend),
		"GET /api/v1/audit":         s.authed(s.globalAudit),

		"GET /api/v1/catalog":   s.authed(s.listCatalog),
		"GET /api/v1/addresses": s.authed(s.listAddresses),

		"POST /api/v1/agent/chat":                  s.authed(s.agentChat),
		"GET /api/v1/agent/sessions/{id}/messages": s.authed(s.agentMessages),
		"GET /api/v1/agent/tools":                  s.authed(s.agentTools),
		"POST /api/v1/agent/tools/{name}":          s.authed(s.agentToolCall),
		"POST /api/v1/realtime/session":            s.authed(s.realtimeSession),

		"POST /api/v1/enrollments":        s.authed(s.startEnrollment),
		"GET /api/v1/enrollments/current": s.authed(s.currentEnrollment),

		"GET /api/v1/admin/catalog":               s.admin(s.adminListCatalog),
		"POST /api/v1/admin/catalog":              s.admin(s.adminCreateCatalog),
		"GET /api/v1/admin/catalog/{id}":          s.admin(s.adminGetCatalog),
		"PUT /api/v1/admin/catalog/{id}":          s.admin(s.adminUpdateCatalog),
		"DELETE /api/v1/admin/catalog/{id}":       s.admin(s.adminDeleteCatalog),
		"GET /api/v1/admin/vendors":               s.admin(s.adminListVendors),
		"POST /api/v1/admin/vendors":              s.admin(s.adminCreateVendor),
		"GET /api/v1/admin/vendors/{id}":          s.admin(s.adminGetVendor),
		"PUT /api/v1/admin/vendors/{id}":          s.admin(s.adminUpdateVendor),
		"DELETE /api/v1/admin/vendors/{id}":       s.admin(s.adminDeleteVendor),
		"GET /api/v1/admin/policy":                s.admin(s.adminGetPolicy),
		"PUT /api/v1/admin/policy":                s.admin(s.adminUpdatePolicy),
		"GET /api/v1/admin/addresses":             s.admin(s.adminListAddresses),
		"POST /api/v1/admin/addresses":            s.admin(s.adminCreateAddress),
		"GET /api/v1/admin/addresses/{id}":        s.admin(s.adminGetAddress),
		"PUT /api/v1/admin/addresses/{id}":        s.admin(s.adminUpdateAddress),
		"DELETE /api/v1/admin/addresses/{id}":     s.admin(s.adminDeleteAddress),
		"GET /api/v1/admin/system-prompt":         s.admin(s.adminGetSystemPrompt),
		"PUT /api/v1/admin/system-prompt":         s.admin(s.adminUpdateSystemPrompt),
		"GET /api/v1/admin/system-prompt/default": s.admin(s.adminDefaultSystemPrompt),

		"POST /api/v1/webhooks/reap": s.reapWebhook,
	}
}

func (s *Server) routes() {
	hs := s.handlers()
	for _, r := range Routes {
		h, ok := hs[r]
		if !ok {
			h = notImplemented
		}
		// Every {id} except chat sessions is a database UUID; anything else cannot exist.
		if strings.Contains(r, "{id}") && !strings.Contains(r, "/sessions/") {
			h = uuidPath(h)
		}
		s.mux.HandleFunc(r, h)
	}
	// Unknown /api paths answer with the JSON error envelope instead of the mux's text 404.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, fmt.Errorf("%w: no route for %s %s", domain.ErrNotFound, r.Method, r.URL.Path))
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"status": "ok"}
	if s.d.Store != nil {
		if err := s.d.Store.Ping(r.Context()); err != nil {
			status["status"], status["db"] = "degraded", "unreachable"
			writeJSON(w, http.StatusServiceUnavailable, status)
			return
		}
		status["db"] = "ok"
	}
	writeJSON(w, http.StatusOK, status)
}

func uuidPath(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !uuidRe.MatchString(r.PathValue("id")) {
			writeError(w, fmt.Errorf("%w: %q is not a valid id", domain.ErrNotFound, r.PathValue("id")))
			return
		}
		h(w, r)
	}
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, domain.ErrNotImplemented)
}

// ---------- middleware ----------

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeError(w, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush on the underlying writer (SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logRequests logs method, path (never the query string: it may carry the token) and status.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

// cors allows the Flutter web dev server. CORS_ORIGINS="*" reflects any Origin (credentials are
// allowed so the cookie works); otherwise only listed origins are reflected.
func (s *Server) cors(next http.Handler) http.Handler {
	allowAll := len(s.d.Config.CORSOrigins) == 0
	allowed := map[string]bool{}
	for _, o := range s.d.Config.CORSOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[strings.TrimRight(o, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- errors and JSON helpers ----------

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// StatusFor maps domain sentinel errors to HTTP status codes and error codes.
func StatusFor(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, "validation_failed"
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, domain.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition"
	case errors.Is(err, domain.ErrNoActiveEnrollment):
		return http.StatusConflict, "no_active_enrollment"
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, domain.ErrUpstream):
		return http.StatusBadGateway, "upstream_error"
	case errors.Is(err, domain.ErrNotImplemented):
		return http.StatusNotImplemented, "not_implemented"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

func writeError(w http.ResponseWriter, err error) {
	status, code := StatusFor(err)
	var body ErrorBody
	body.Error.Code = code
	body.Error.Message = err.Error()
	if status == http.StatusInternalServerError {
		body.Error.Message = "internal error"
	}
	writeJSON(w, status, body)
}

// fail writes err and logs internal errors (the client only sees "internal error").
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if status, _ := StatusFor(err); status >= 500 && status != http.StatusNotImplemented {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeError(w, err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

const maxBody = 1 << 20

// decode reads a JSON body into v. An empty body is allowed only when optional is true.
func decode(r *http.Request, v any, optional bool) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", domain.ErrValidation, err)
	}
	if len(b) > maxBody {
		return fmt.Errorf("%w: body too large", domain.ErrValidation)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		if optional {
			return nil
		}
		return fmt.Errorf("%w: request body is required", domain.ErrValidation)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", domain.ErrValidation, err)
	}
	return nil
}

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrValidation, fmt.Sprintf(format, args...))
}

// page parses limit (1..200, default 50) and offset (>= 0, default 0).
func page(r *http.Request) (limit, offset int, err error) {
	limit, offset = 50, 0
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 200 {
			return 0, 0, validationf("limit must be an integer between 1 and 200")
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 {
			return 0, 0, validationf("offset must be a non-negative integer")
		}
		offset = n
	}
	return limit, offset, nil
}

// need returns a 501 error when a collaborator is not wired.
func need(ok bool, what string) error {
	if !ok {
		return fmt.Errorf("%w: %s is not configured", domain.ErrNotImplemented, what)
	}
	return nil
}
