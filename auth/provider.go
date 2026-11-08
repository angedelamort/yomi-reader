package auth

import "net/http"

// User is the authenticated identity returned by a Provider.
type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"isAdmin"`
}

// Provider abstracts an authentication backend (local, Plex, or dev mock).
type Provider interface {
	// Name returns the provider identifier ("local", "plex", or "mock").
	Name() string

	// StartLogin begins the redirect login flow and returns the URL the browser
	// should be sent to. Providers without an external step return the local
	// callback (mock) or the SPA root (local).
	StartLogin(w http.ResponseWriter, r *http.Request) (redirectURL string, err error)

	// HandleCallback completes the redirect login flow and returns the user.
	HandleCallback(w http.ResponseWriter, r *http.Request) (*User, error)
}

// PasswordAuthenticator verifies a username/password pair. It backs both the
// JSON login endpoint and HTTP Basic auth on the OPDS routes.
type PasswordAuthenticator interface {
	Authenticate(username, password string) (*User, error)
}
