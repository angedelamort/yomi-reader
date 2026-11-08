package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

type ctxKey int

const userCtxKey ctxKey = 0

// UserCheck re-validates a session against the user database. It returns the
// current admin flag and ok=false when the user no longer exists.
type UserCheck func(username string) (isAdmin bool, ok bool)

// Middleware wires session verification into the HTTP handler chain.
type Middleware struct {
	sessions *SessionManager
	check    UserCheck
	basic    PasswordAuthenticator
}

// NewMiddleware creates auth middleware backed by the given session manager.
func NewMiddleware(s *SessionManager) *Middleware {
	return &Middleware{sessions: s}
}

// SetUserCheck installs a hook that refreshes the admin flag from the database
// on every request and rejects sessions of deleted users.
func (m *Middleware) SetUserCheck(fn UserCheck) { m.check = fn }

// SetBasicAuthenticator enables HTTP Basic auth in RequireAuthOrBasic.
func (m *Middleware) SetBasicAuthenticator(p PasswordAuthenticator) { m.basic = p }

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sessionFromCookie reads and re-validates the cookie session.
func (m *Middleware) sessionFromCookie(w http.ResponseWriter, r *http.Request) *Session {
	s, err := m.sessions.Read(r)
	if err != nil {
		return nil
	}
	if m.check != nil {
		isAdmin, ok := m.check(s.Username)
		if !ok {
			m.sessions.Clear(w)
			return nil
		}
		s.IsAdmin = isAdmin
	}
	return s
}

// RequireAuth rejects requests without a valid session (401) and injects the
// session into the request context.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.sessionFromCookie(w, r)
		if s == nil {
			jsonError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, s)))
	})
}

// RequireAdmin requires a valid session whose user is an admin (403 otherwise).
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := FromContext(r.Context())
		if s == nil || !s.IsAdmin {
			jsonError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireAuthOrBasic accepts either the session cookie or HTTP Basic
// credentials (for OPDS clients). Unauthenticated requests get a Basic
// challenge.
func (m *Middleware) RequireAuthOrBasic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.sessionFromCookie(w, r)
		if s == nil && m.basic != nil {
			if user, pass, ok := r.BasicAuth(); ok {
				if u, err := m.basic.Authenticate(user, pass); err == nil {
					s = &Session{Username: u.Username, Email: u.Email, IsAdmin: u.IsAdmin}
					if m.check != nil {
						isAdmin, ok := m.check(s.Username)
						if !ok {
							s = nil
						} else {
							s.IsAdmin = isAdmin
						}
					}
				}
			}
		}
		if s == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Yomi Reader", charset="UTF-8"`)
			jsonError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, s)))
	})
}

// FromContext returns the session stored in the request context, or nil.
func FromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(userCtxKey).(*Session)
	return s
}
