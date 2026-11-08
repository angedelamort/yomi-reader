package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	plexPinsURL     = "https://plex.tv/api/v2/pins"
	plexUserURL     = "https://plex.tv/api/v2/user"
	plexResourceURL = "https://plex.tv/api/v2/resources"
	plexAuthAppURL  = "https://app.plex.tv/auth"
	pinCookieName   = "yr_plex_pin"
)

// PlexProvider implements Plex SSO via PIN (server redirect or client popup).
type PlexProvider struct {
	// Product is the human-readable app name shown on plex.tv.
	Product string
	// MachineID is the Plex Media Server machineIdentifier used for ACL.
	MachineID string
	// PublicBaseURL, when set, is used as the redirect base for the server PIN
	// callback. When empty, the base is derived from the request (and proxy headers).
	PublicBaseURL string
	// Secure controls the Secure flag on the temporary PIN cookie.
	Secure bool

	http *http.Client
}

// NewPlexProvider builds a Plex auth provider.
func NewPlexProvider(product, machineID, publicBaseURL string, secure bool) *PlexProvider {
	if product == "" {
		product = "Yomi Reader"
	}
	return &PlexProvider{
		Product:       product,
		MachineID:     strings.TrimSpace(machineID),
		PublicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		Secure:        secure,
		http:          &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *PlexProvider) Name() string { return "plex" }

func (p *PlexProvider) clientID() string {
	return "yomi-reader-" + p.MachineID
}

func (p *PlexProvider) baseURLForRequest(r *http.Request) string {
	if p.PublicBaseURL != "" {
		return p.PublicBaseURL
	}

	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	host = strings.TrimSpace(strings.Split(host, ",")[0])

	return strings.TrimRight(scheme+"://"+host, "/")
}

type plexPin struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
}

func (p *PlexProvider) plexHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", p.Product)
	req.Header.Set("X-Plex-Client-Identifier", p.clientID())
}

// StartLogin requests a PIN from plex.tv and returns the plex.tv auth URL.
func (p *PlexProvider) StartLogin(w http.ResponseWriter, r *http.Request) (string, error) {
	if p.MachineID == "" {
		return "", errors.New("PLEX_MACHINE_ID is not configured")
	}
	form := url.Values{"strong": {"true"}}
	req, err := http.NewRequest(http.MethodPost, plexPinsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	p.plexHeaders(req)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request plex pin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plex pin request failed: status %d", resp.StatusCode)
	}

	var pin plexPin
	if err := json.NewDecoder(resp.Body).Decode(&pin); err != nil {
		return "", fmt.Errorf("decode plex pin: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     pinCookieName,
		Value:    strconv.Itoa(pin.ID),
		Path:     "/",
		HttpOnly: true,
		Secure:   p.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	forwardURL := p.baseURLForRequest(r) + "/auth/callback"
	authURL := plexAuthAppURL + "#?" + url.Values{
		"clientID":                 {p.clientID()},
		"code":                     {pin.Code},
		"forwardUrl":               {forwardURL},
		"context[device][product]": {p.Product},
	}.Encode()

	return authURL, nil
}

// HandleCallback exchanges the stored PIN for an auth token, then validates
// access to the configured Plex server.
func (p *PlexProvider) HandleCallback(w http.ResponseWriter, r *http.Request) (*User, error) {
	c, err := r.Cookie(pinCookieName)
	if err != nil {
		return nil, errors.New("missing plex pin cookie")
	}
	http.SetCookie(w, &http.Cookie{Name: pinCookieName, Value: "", Path: "/", MaxAge: -1})

	token, err := p.pollToken(c.Value)
	if err != nil {
		return nil, err
	}
	return p.userFromToken(token)
}

// ClientConfig is what the browser needs for the popup PIN flow.
type ClientConfig struct {
	Product string `json:"product"`
}

// ClientConfig returns the configuration the frontend popup flow needs.
func (p *PlexProvider) ClientConfig() ClientConfig {
	return ClientConfig{Product: p.Product}
}

// ExchangeToken validates a Plex auth token from the client-side popup flow.
func (p *PlexProvider) ExchangeToken(token string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("missing plex token")
	}
	return p.userFromToken(token)
}

func (p *PlexProvider) userFromToken(token string) (*User, error) {
	if p.MachineID == "" {
		return nil, errors.New("PLEX_MACHINE_ID is not configured")
	}
	u, err := p.fetchUser(token)
	if err != nil {
		return nil, err
	}
	isAdmin, hasAccess, err := p.checkServerAccess(token)
	if err != nil {
		return nil, err
	}
	if !hasAccess && !isAdmin {
		return nil, errors.New("user has no access to this Plex server")
	}
	username := u.Username
	if username == "" {
		username = u.Title
	}
	return &User{Username: username, Email: u.Email, IsAdmin: isAdmin}, nil
}

func (p *PlexProvider) pollToken(pinID string) (string, error) {
	u := fmt.Sprintf("%s/%s", plexPinsURL, pinID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	p.plexHeaders(req)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("poll plex pin: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		AuthToken string `json:"authToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode plex pin token: %w", err)
	}
	if out.AuthToken == "" {
		return "", errors.New("plex authentication not completed")
	}
	return out.AuthToken, nil
}

type plexUser struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Title    string `json:"title"`
}

func (p *PlexProvider) fetchUser(token string) (*plexUser, error) {
	req, err := http.NewRequest(http.MethodGet, plexUserURL, nil)
	if err != nil {
		return nil, err
	}
	p.plexHeaders(req)
	req.Header.Set("X-Plex-Token", token)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch plex user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch plex user failed: status %d", resp.StatusCode)
	}

	var u plexUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode plex user: %w", err)
	}
	return &u, nil
}

type plexResource struct {
	ClientIdentifier string `json:"clientIdentifier"`
	Owned            bool   `json:"owned"`
	Provides         string `json:"provides"`
}

func (p *PlexProvider) checkServerAccess(token string) (isAdmin, hasAccess bool, err error) {
	u := plexResourceURL + "?" + url.Values{"includeHttps": {"1"}}.Encode()
	req, reqErr := http.NewRequest(http.MethodGet, u, nil)
	if reqErr != nil {
		return false, false, reqErr
	}
	p.plexHeaders(req)
	req.Header.Set("X-Plex-Token", token)

	resp, doErr := p.http.Do(req)
	if doErr != nil {
		return false, false, fmt.Errorf("fetch plex resources: %w", doErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false, fmt.Errorf("fetch plex resources failed: status %d", resp.StatusCode)
	}

	var resources []plexResource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return false, false, fmt.Errorf("decode plex resources: %w", err)
	}

	for _, res := range resources {
		if res.ClientIdentifier == p.MachineID {
			return res.Owned, true, nil
		}
	}
	return false, false, nil
}
