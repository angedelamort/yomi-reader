package auth

import "net/http"

// MockProvider is a development auth provider that logs in automatically as a
// fixed configured user without any password check.
type MockProvider struct {
	Username string
	IsAdmin  bool
}

// NewMockProvider creates a mock provider for the given user.
func NewMockProvider(username string, isAdmin bool) *MockProvider {
	if username == "" {
		username = "dev"
	}
	return &MockProvider{Username: username, IsAdmin: isAdmin}
}

// Name implements Provider.
func (p *MockProvider) Name() string { return "mock" }

// StartLogin skips any external step and goes straight to the callback.
func (p *MockProvider) StartLogin(w http.ResponseWriter, r *http.Request) (string, error) {
	return "/auth/callback", nil
}

// HandleCallback returns the configured mock user.
func (p *MockProvider) HandleCallback(w http.ResponseWriter, r *http.Request) (*User, error) {
	return p.User(), nil
}

// User returns the mock identity.
func (p *MockProvider) User() *User {
	return &User{
		Username: p.Username,
		Email:    p.Username + "@mock.local",
		IsAdmin:  p.IsAdmin,
	}
}

// Authenticate accepts the mock user with any password (dev only), which lets
// OPDS clients using Basic auth work in mock mode.
func (p *MockProvider) Authenticate(username, password string) (*User, error) {
	if username != p.Username {
		return nil, ErrInvalidCredentials
	}
	return p.User(), nil
}
