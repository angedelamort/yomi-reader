package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionCookieName is the name of the signed session cookie.
const SessionCookieName = "yr_session"

// sessionKeyFile is the filename, under the data dir, where an auto-generated
// session secret is persisted so cookies survive restarts.
const sessionKeyFile = "session.key"

// ResolveSessionSecret returns the cookie-signing secret, preferring an
// explicit value (SESSION_SECRET) and otherwise reading a persisted key from
// dataDir, generating and storing a new random one on first run.
func ResolveSessionSecret(explicit, dataDir string) (string, error) {
	if s := strings.TrimSpace(explicit); s != "" {
		return s, nil
	}

	path := filepath.Join(dataDir, sessionKeyFile)
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read session key: %w", err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session key: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("persist session key: %w", err)
	}
	return secret, nil
}

// sessionTTL is how long an issued session remains valid.
const sessionTTL = 30 * 24 * time.Hour

// Session is the signed payload stored in the cookie.
type Session struct {
	Username string `json:"u"`
	Email    string `json:"e"`
	IsAdmin  bool   `json:"a"`
	Expires  int64  `json:"x"`
}

// SessionManager signs and verifies session cookies using HMAC-SHA256.
type SessionManager struct {
	secret []byte
	secure bool
}

// NewSessionManager creates a manager with the given signing secret. secure
// controls the Secure cookie flag (disable for plain-HTTP deployments).
func NewSessionManager(secret string, secure bool) *SessionManager {
	return &SessionManager{secret: []byte(secret), secure: secure}
}

func (m *SessionManager) sign(payload []byte) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Set issues a signed session cookie for the given user.
func (m *SessionManager) Set(w http.ResponseWriter, u *User) error {
	s := Session{
		Username: u.Username,
		Email:    u.Email,
		IsAdmin:  u.IsAdmin,
		Expires:  time.Now().Add(sessionTTL).Unix(),
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	value := encoded + "." + m.sign(payload)

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(s.Expires, 0),
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

// Read verifies and decodes the session cookie from the request.
func (m *SessionManager) Read(r *http.Request) (*Session, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, errors.New("no session cookie")
	}

	idx := strings.LastIndexByte(c.Value, '.')
	if idx <= 0 || idx == len(c.Value)-1 {
		return nil, errors.New("malformed session cookie")
	}
	encoded, sig := c.Value[:idx], c.Value[idx+1:]

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}

	expected := m.sign(payload)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return nil, errors.New("invalid session signature")
	}

	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	if time.Now().Unix() > s.Expires {
		return nil, errors.New("session expired")
	}
	return &s, nil
}

// Clear removes the session cookie.
func (m *SessionManager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
