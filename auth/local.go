package auth

import (
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is returned for unknown users and wrong passwords
	// alike, so callers cannot enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrUserNotFound is returned by CredentialStore when a user does not exist.
	ErrUserNotFound = errors.New("user not found")
)

// CredentialStore looks up a user and their bcrypt password hash.
type CredentialStore interface {
	LookupCredentials(username string) (user *User, passwordHash string, err error)
}

// LocalProvider authenticates users against bcrypt hashes in the database.
type LocalProvider struct {
	store CredentialStore
}

// NewLocalProvider creates a local username/password provider.
func NewLocalProvider(store CredentialStore) *LocalProvider {
	return &LocalProvider{store: store}
}

// Name implements Provider.
func (p *LocalProvider) Name() string { return "local" }

// StartLogin sends the browser to the SPA, which renders its own login form.
func (p *LocalProvider) StartLogin(w http.ResponseWriter, r *http.Request) (string, error) {
	return "/", nil
}

// HandleCallback is not used by the local provider.
func (p *LocalProvider) HandleCallback(w http.ResponseWriter, r *http.Request) (*User, error) {
	return nil, errors.New("local provider: use POST /api/auth/login")
}

// dummyHash is compared against when the user does not exist, keeping response
// time similar for unknown users and wrong passwords.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("yomi-reader-dummy"), bcrypt.DefaultCost)

// Authenticate verifies the credentials and returns the stored user.
func (p *LocalProvider) Authenticate(username, password string) (*User, error) {
	u, hash, err := p.store.LookupCredentials(username)
	if err != nil || hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		if err != nil && !errors.Is(err, ErrUserNotFound) {
			return nil, err
		}
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// HashPassword returns a bcrypt hash for the given password.
func HashPassword(password string) (string, error) {
	// bcrypt only considers the first 72 bytes; reject longer input instead of
	// silently truncating it.
	if len(password) > 72 {
		return "", errors.New("password too long (max 72 bytes)")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
