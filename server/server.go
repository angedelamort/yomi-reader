// Package server wires HTTP routes for the API, OPDS feeds and the static SPA.
package server

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"yomi-reader/auth"
	"yomi-reader/library"
)

// Deps holds the wired dependencies a server needs.
type Deps struct {
	Provider  auth.Provider
	Sessions  *auth.SessionManager
	Mw        *auth.Middleware
	Library   *library.Handler
	StaticDir string
}

func writeJSONOK(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// loginLimiter throttles failed password attempts per client IP + username.
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limitEntry
}

type limitEntry struct {
	count int
	reset time.Time
}

const (
	maxLoginFailures = 8
	loginWindow      = 10 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{entries: map[string]*limitEntry{}} }

func (l *loginLimiter) key(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host + "|" + strings.ToLower(username)
}

// blocked reports whether the key has exhausted its attempts.
func (l *loginLimiter) blocked(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[k]
	if !ok {
		return false
	}
	if time.Now().After(e.reset) {
		delete(l.entries, k)
		return false
	}
	return e.count >= maxLoginFailures
}

func (l *loginLimiter) fail(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// opportunistic cleanup
	if len(l.entries) > 1000 {
		for key, e := range l.entries {
			if now.After(e.reset) {
				delete(l.entries, key)
			}
		}
	}
	e, ok := l.entries[k]
	if !ok || now.After(e.reset) {
		e = &limitEntry{reset: now.Add(loginWindow)}
		l.entries[k] = e
	}
	e.count++
}

func (l *loginLimiter) clear(k string) {
	l.mu.Lock()
	delete(l.entries, k)
	l.mu.Unlock()
}

// NewMux builds the application's HTTP routes.
func NewMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	lib := d.Library
	limiter := newLoginLimiter()

	auth_ := func(h http.HandlerFunc) http.Handler { return d.Mw.RequireAuth(h) }
	admin := func(h http.HandlerFunc) http.Handler { return d.Mw.RequireAdmin(h) }
	opds := func(h http.HandlerFunc) http.Handler { return d.Mw.RequireAuthOrBasic(h) }

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})

	// --- Auth ---
	mux.HandleFunc("GET /api/auth/info", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{"provider": d.Provider.Name(), "authenticated": false}
		if s, err := d.Sessions.Read(r); err == nil {
			if isAdmin, ok := lib.Store().UserCheck(s.Username); ok {
				info["authenticated"] = true
				info["user"] = map[string]any{"username": s.Username, "email": s.Email, "isAdmin": isAdmin}
			}
		}
		writeJSONOK(w, info)
	})

	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		pa, ok := d.Provider.(auth.PasswordAuthenticator)
		if !ok {
			writeJSONErr(w, http.StatusBadRequest, "password login is not available; use /auth/login")
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		in.Username = strings.TrimSpace(in.Username)
		if in.Username == "" || in.Password == "" {
			writeJSONErr(w, http.StatusBadRequest, "username and password are required")
			return
		}
		k := limiter.key(r, in.Username)
		if limiter.blocked(k) {
			w.Header().Set("Retry-After", "600")
			writeJSONErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
			return
		}
		user, err := pa.Authenticate(in.Username, in.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				limiter.fail(k)
				writeJSONErr(w, http.StatusUnauthorized, "invalid username or password")
			} else {
				log.Printf("login error for %q: %v", in.Username, err)
				writeJSONErr(w, http.StatusInternalServerError, "login failed")
			}
			return
		}
		limiter.clear(k)
		if err := finishLogin(w, d, user); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "session error")
			return
		}
		writeJSONOK(w, map[string]any{"ok": true, "user": user})
	})

	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		d.Sessions.Clear(w)
		writeJSONOK(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		url, err := d.Provider.StartLogin(w, r)
		if err != nil {
			http.Error(w, "login failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		user, err := d.Provider.HandleCallback(w, r)
		if err != nil {
			http.Error(w, "authentication failed: "+err.Error(), http.StatusUnauthorized)
			return
		}
		if err := finishLogin(w, d, user); err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("GET /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		d.Sessions.Clear(w)
		http.Redirect(w, r, "/", http.StatusFound)
	})

	// --- Current user ---
	mux.Handle("GET /api/me", auth_(lib.Me))
	mux.Handle("GET /api/preferences", auth_(lib.GetPreferences))
	mux.Handle("PUT /api/preferences", auth_(lib.PutPreferences))
	mux.Handle("GET /api/libraries", auth_(lib.ListLibraries))

	// --- Catalog ---
	mux.Handle("GET /api/home", auth_(lib.Home))
	mux.Handle("GET /api/series", auth_(lib.ListSeries))
	mux.Handle("GET /api/series/{id}", auth_(lib.GetSeries))
	mux.Handle("PUT /api/series/{id}/reader-mode", auth_(lib.PutSeriesReaderMode))
	mux.Handle("GET /api/series/{id}/chapters", auth_(lib.ListChapters))
	mux.Handle("GET /api/chapters/{id}", auth_(lib.GetChapter))
	mux.Handle("GET /api/chapters/{id}/pages", auth_(lib.ChapterPages))
	mux.Handle("GET /api/chapters/{id}/page/{n}", auth_(lib.ChapterPage))
	mux.Handle("GET /api/chapters/{id}/file", auth_(lib.ChapterFile))
	mux.Handle("GET /api/covers/{seriesId}", auth_(lib.Cover))
	mux.Handle("GET /api/tags", auth_(lib.ListTags))
	mux.Handle("GET /api/sources", auth_(lib.ListSources))

	// --- Progress / favorites ---
	mux.Handle("GET /api/progress/{seriesId}", auth_(lib.GetProgress))
	mux.Handle("PUT /api/progress/{seriesId}", auth_(lib.PutProgress))
	mux.Handle("DELETE /api/progress/{seriesId}", auth_(lib.DeleteProgress))
	mux.Handle("GET /api/favorites", auth_(lib.ListFavorites))
	mux.Handle("PUT /api/favorites/{seriesId}", auth_(lib.SetFavorite))
	mux.Handle("DELETE /api/favorites/{seriesId}", auth_(lib.SetFavorite))

	// --- Admin ---
	mux.Handle("GET /api/admin/browse", admin(lib.AdminBrowseDirs))
	mux.Handle("GET /api/admin/libraries", admin(lib.AdminListLibraries))
	mux.Handle("POST /api/admin/libraries", admin(lib.AdminCreateLibrary))
	mux.Handle("PUT /api/admin/libraries/{id}", admin(lib.AdminUpdateLibrary))
	mux.Handle("DELETE /api/admin/libraries/{id}", admin(lib.AdminDeleteLibrary))
	mux.Handle("POST /api/admin/libraries/{id}/scan", admin(lib.AdminScanLibrary))
	mux.Handle("GET /api/admin/libraries/{id}/scan-progress", admin(lib.AdminScanProgress))
	mux.Handle("GET /api/admin/sources", admin(lib.AdminListSources))
	mux.Handle("GET /api/admin/jobs", admin(lib.AdminListJobs))
	mux.Handle("PUT /api/admin/series/{id}", admin(lib.AdminUpdateSeries))
	mux.Handle("PUT /api/admin/series/{id}/cover", admin(lib.AdminSetSeriesCover))
	mux.Handle("POST /api/admin/series/{id}/cover", admin(lib.AdminSetSeriesCover))
	mux.Handle("GET /api/admin/users", admin(lib.AdminListUsers))
	mux.Handle("POST /api/admin/users", admin(lib.AdminCreateUser))
	mux.Handle("PUT /api/admin/users/{username}", admin(lib.AdminUpdateUser))
	mux.Handle("DELETE /api/admin/users/{username}", admin(lib.AdminDeleteUser))
	mux.Handle("GET /api/admin/settings", admin(lib.AdminGetSettings))
	mux.Handle("PUT /api/admin/settings", admin(lib.AdminUpdateSettings))

	// --- OPDS (session cookie or HTTP Basic) ---
	mux.Handle("GET /opds", opds(lib.OPDSRoot))
	mux.Handle("GET /opds/{$}", opds(lib.OPDSRoot))
	mux.Handle("GET /opds/browse", opds(lib.OPDSBrowse))
	mux.Handle("GET /opds/search.xml", opds(lib.OPDSSearchDescription))
	mux.Handle("GET /opds/series/{id}", opds(lib.OPDSSeries))
	mux.Handle("GET /opds/covers/{seriesId}", opds(lib.Cover))
	mux.Handle("GET /opds/chapters/{id}/file", opds(lib.ChapterFile))
	mux.Handle("GET /opds/chapters/{id}/page/{n}", opds(lib.ChapterPage))

	// --- Static SPA with index.html fallback ---
	staticDir := d.StaticDir
	if staticDir == "" {
		staticDir = "static"
	}
	fileServer := setNoCache(http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/auth/") || strings.HasPrefix(p, "/opds/") {
			writeJSONErr(w, http.StatusNotFound, "not found")
			return
		}
		index := filepath.Join(staticDir, "index.html")
		if p == "/" {
			noCacheHeaders(w)
			http.ServeFile(w, r, index)
			return
		}
		clean := filepath.Join(staticDir, filepath.FromSlash(filepath.ToSlash(filepath.Clean("/"+p))))
		if st, err := os.Stat(clean); err != nil || st.IsDir() {
			noCacheHeaders(w)
			http.ServeFile(w, r, index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	return mux
}

// finishLogin records the login and issues the session cookie.
func finishLogin(w http.ResponseWriter, d Deps, user *auth.User) error {
	if err := d.Library.RecordLogin(user, d.Provider.Name()); err != nil {
		log.Printf("record login for %s: %v", user.Username, err)
	}
	// Prefer the canonical admin flag from the database.
	if isAdmin, ok := d.Library.Store().UserCheck(user.Username); ok {
		user.IsAdmin = isAdmin
	}
	return d.Sessions.Set(w, user)
}

func noCacheHeaders(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-cache") }

func setNoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noCacheHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer so streaming responses keep working.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// LogRequests logs API/OPDS requests (static assets and page images are skipped
// to keep the log readable) and sets baseline security headers.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.Contains(r.URL.Path, "/page/") || strings.HasPrefix(r.URL.Path, "/api/covers/") {
			if rec.status < 400 {
				return
			}
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
