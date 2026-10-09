package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// TokenCookie is the cookie set by POST /auth/login (fake auth; the value is the user id).
const TokenCookie = "jarvis_token"

type ctxKey int

const userKey ctxKey = 1

// UserFrom returns the authenticated user put in the context by the auth middleware.
func UserFrom(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userKey).(domain.User)
	return u, ok
}

// WithUser returns ctx carrying u (used by tests and internal callers).
func WithUser(ctx context.Context, u domain.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// tokenFrom extracts the fake token: Bearer header, then cookie, then ?token=.
func tokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if scheme, tok, ok := strings.Cut(h, " "); ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(tok)
		}
	}
	if c, err := r.Cookie(TokenCookie); err == nil && c.Value != "" {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

func (s *Server) authenticate(r *http.Request) (domain.User, error) {
	tok := tokenFrom(r)
	if tok == "" {
		return domain.User{}, fmt.Errorf("%w: missing bearer token (POST /api/v1/auth/login first)", domain.ErrUnauthorized)
	}
	if !uuidRe.MatchString(tok) {
		return domain.User{}, fmt.Errorf("%w: unknown token", domain.ErrUnauthorized)
	}
	if s.d.Store == nil {
		return domain.User{}, need(false, "store")
	}
	u, err := s.d.Store.Users().Get(r.Context(), tok)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, fmt.Errorf("%w: unknown token", domain.ErrUnauthorized)
	}
	return u, err
}

// authed requires a valid fake token and puts the user in the request context.
func (s *Server) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.authenticate(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		h(w, r.WithContext(WithUser(r.Context(), u)))
	}
}

// roles requires one of the given roles (use inside authed).
func (s *Server) roles(h http.HandlerFunc, allowed ...domain.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		for _, a := range allowed {
			if u.Role == a {
				h(w, r)
				return
			}
		}
		s.fail(w, r, fmt.Errorf("%w: role %q cannot do this", domain.ErrForbidden, u.Role))
	}
}

// admin requires an admin user.
func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return s.authed(s.roles(h, domain.RoleAdmin))
}

func currentUser(r *http.Request) domain.User {
	u, _ := UserFrom(r.Context())
	return u
}

// LoginBody is the fake login. Email logs in an existing user. When the email is unknown (or
// omitted) and both name and role are given, a demo user is created on the fly ("any name").
type LoginBody struct {
	Email string      `json:"email"`
	Name  string      `json:"name"`
	Role  domain.Role `json:"role"`
}

// LoginResponse is {token, user}.
type LoginResponse struct {
	Token string      `json:"token"`
	User  domain.User `json:"user"`
}

func validRole(r domain.Role) bool {
	return r == domain.RoleManager || r == domain.RoleApprover || r == domain.RoleAdmin
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Store != nil, "store"); err != nil {
		s.fail(w, r, err)
		return
	}
	var in LoginBody
	if err := decode(r, &in, false); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if in.Email == "" && in.Name == "" {
		s.fail(w, r, validationf("email is required"))
		return
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			s.fail(w, r, validationf("email is not valid"))
			return
		}
	}
	users := s.d.Store.Users()
	var (
		u   domain.User
		err error
	)
	if in.Email != "" {
		u, err = users.GetByEmail(r.Context(), in.Email)
	} else {
		err = domain.ErrNotFound
	}
	if errors.Is(err, domain.ErrNotFound) && in.Name != "" {
		if !validRole(in.Role) {
			s.fail(w, r, validationf("role must be manager, approver or admin"))
			return
		}
		email := in.Email
		if email == "" {
			slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(in.Name), "."), ".")
			if slug == "" {
				slug = "user"
			}
			email = fmt.Sprintf("%s.%s@demo.jarvis.local", slug, in.Role)
		}
		u = domain.User{Name: in.Name, Email: email, Role: in.Role}
		err = users.Upsert(r.Context(), &u)
		if err == nil && u.ID == "" {
			u, err = users.GetByEmail(r.Context(), email)
		}
	}
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			err = fmt.Errorf("%w: no user with that email (pick a demo user or give name and role)", domain.ErrNotFound)
		}
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: TokenCookie, Value: u.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 3600,
	})
	_ = s.d.Audit.Record(r.Context(), "", domain.ActorUser, u.ID, audit.UserLoggedIn, map[string]any{"role": u.Role})
	writeJSON(w, http.StatusOK, LoginResponse{Token: u.ID, User: u})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Store != nil, "store"); err != nil {
		s.fail(w, r, err)
		return
	}
	us, err := s.d.Store.Users().List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if us == nil {
		us = []domain.User{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": us})
}
