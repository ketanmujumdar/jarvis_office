package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

func TestLogin(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
		wantRole domain.Role
		wantErr  string
	}{
		{"existing email", `{"email":"maya@jarvis.example"}`, 200, domain.RoleManager, ""},
		{"email case-insensitive", `{"email":" Daniel@Jarvis.Example "}`, 200, domain.RoleApprover, ""},
		{"unknown email", `{"email":"ghost@jarvis.example"}`, 404, "", "not_found"},
		{"any name creates demo user", `{"name":"Alex Lim","role":"approver"}`, 200, domain.RoleApprover, ""},
		{"unknown email with name creates", `{"email":"new@jarvis.example","name":"New","role":"admin"}`, 200, domain.RoleAdmin, ""},
		{"name with bad role", `{"name":"Alex","role":"ceo"}`, 400, "", "validation_failed"},
		{"bad email", `{"email":"not-an-email"}`, 400, "", "validation_failed"},
		{"empty object", `{}`, 400, "", "validation_failed"},
		{"no body", ``, 400, "", "validation_failed"},
		{"bad json", `{`, 400, "", "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			r := h.do("POST", "/api/v1/auth/login", "", tt.body)
			if r.code != tt.wantCode {
				t.Fatalf("code = %d body %s", r.code, r.body)
			}
			if tt.wantErr != "" {
				if got := r.errCode(t); got != tt.wantErr {
					t.Fatalf("error code = %s", got)
				}
				return
			}
			var out LoginResponse
			r.json(t, &out)
			if out.Token == "" || out.Token != out.User.ID || out.User.Role != tt.wantRole {
				t.Fatalf("login = %+v", out)
			}
			if !strings.Contains(r.hdr.Get("Set-Cookie"), TokenCookie+"="+out.Token) {
				t.Fatalf("cookie = %q", r.hdr.Get("Set-Cookie"))
			}
			// The token works for /me.
			me := h.do("GET", "/api/v1/auth/me", out.Token, nil)
			if me.code != 200 || !strings.Contains(string(me.body), out.User.ID) {
				t.Fatalf("me = %d %s", me.code, me.body)
			}
		})
	}
}

func TestLoginTwiceWithSameNameReusesUser(t *testing.T) {
	h := newHarness(t)
	var a, b LoginResponse
	h.do("POST", "/api/v1/auth/login", "", `{"name":"Alex Lim","role":"manager"}`).json(t, &a)
	h.do("POST", "/api/v1/auth/login", "", `{"name":"Alex Lim","role":"manager"}`).json(t, &b)
	if a.User.ID == "" || a.User.ID != b.User.ID || a.User.Email != "alex.lim.manager@demo.jarvis.local" {
		t.Fatalf("a=%+v b=%+v", a.User, b.User)
	}
}

func TestAuthTokenSources(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name  string
		setup func(*http.Request)
		want  int
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+managerID) }, 200},
		{"bearer lowercase scheme", func(r *http.Request) { r.Header.Set("Authorization", "bearer "+managerID) }, 200},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: TokenCookie, Value: adminID}) }, 200},
		{"query token", func(r *http.Request) { r.URL.RawQuery = "token=" + approverID }, 200},
		{"none", func(r *http.Request) {}, 401},
		{"not a uuid", func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc") }, 401},
		{"unknown user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+missingID) }, 401},
		{"basic scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+managerID) }, 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
			tt.setup(req)
			rec := httptest.NewRecorder()
			h.h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("code = %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestListUsersIsPublic(t *testing.T) {
	h := newHarness(t)
	var out struct{ Users []domain.User }
	r := h.do("GET", "/api/v1/auth/users", "", nil)
	r.json(t, &out)
	if r.code != 200 || len(out.Users) != 3 {
		t.Fatalf("users = %d %s", r.code, r.body)
	}
}

// Role matrix: who may call what (403 = forbidden, anything else = allowed past the role check).
func TestRoleMatrix(t *testing.T) {
	tests := []struct {
		method, path string
		manager      bool
		approver     bool
		admin        bool
	}{
		{"POST", "/api/v1/requests", true, false, true},
		{"POST", "/api/v1/requests/" + requestID + "/confirm", true, false, true},
		{"POST", "/api/v1/requests/" + requestID + "/cancel", true, false, true},
		{"GET", "/api/v1/requests", true, true, true},
		{"GET", "/api/v1/approvals", false, true, true},
		{"POST", "/api/v1/approvals/" + approvalID + "/approve", false, true, true},
		{"POST", "/api/v1/approvals/" + approvalID + "/reject", false, true, true},
		{"GET", "/api/v1/approvals/" + approvalID, true, true, true},
		{"GET", "/api/v1/admin/catalog", false, false, true},
		{"PUT", "/api/v1/admin/policy", false, false, true},
		{"GET", "/api/v1/admin/system-prompt", false, false, true},
		{"DELETE", "/api/v1/admin/vendors/" + vendorID, false, false, true},
		{"GET", "/api/v1/orders", true, true, true},
		{"POST", "/api/v1/enrollments", true, true, true},
	}
	for _, tt := range tests {
		for _, who := range []struct {
			id      string
			allowed bool
		}{{managerID, tt.manager}, {approverID, tt.approver}, {adminID, tt.admin}} {
			h := newHarness(t)
			r := h.do(tt.method, tt.path, who.id, `{}`)
			if forbidden := r.code == 403; forbidden == who.allowed {
				t.Errorf("%s %s as %s: code %d (allowed=%v) %s", tt.method, tt.path, h.st.users[who.id].Role, r.code, who.allowed, r.body)
			}
		}
		// Anonymous is always 401.
		if r := newHarness(t).do(tt.method, tt.path, "", `{}`); r.code != 401 {
			t.Errorf("%s %s anonymous = %d", tt.method, tt.path, r.code)
		}
	}
}
