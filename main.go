package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"yomi-reader/auth"
	"yomi-reader/config"
	"yomi-reader/library"
	"yomi-reader/server"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// bootstrapLibraryPath is registered automatically on first run when it exists
// and DEFAULT_LIBRARY_PATH is not set.
const bootstrapLibraryPath = `M:\books\suwayomi`

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	coverDir := filepath.Join(cfg.DataPath, "cache", "covers")
	for _, dir := range []string{cfg.DataPath, coverDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create %s: %v", dir, err)
		}
	}

	db, err := library.OpenDB(cfg.DataPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	store := library.NewStore(db)
	store.MarkInterruptedJobs()

	covers := library.NewCoverService(store, coverDir, cfg.ThumbWidth)
	covers.Start(2)
	scanner := library.NewScanner(store, covers)
	autoScan := library.NewAutoScanner(store, scanner, cfg.AutoScanWatch)

	handler := library.NewHandler(store, scanner, covers)
	handler.SetAutoScanner(autoScan)

	// --- Auth wiring ---
	secureCookies := cfg.AuthProvider != "mock" && cfg.CookieSecure
	sessionSecret, err := auth.ResolveSessionSecret(cfg.SessionSecret, cfg.DataPath)
	if err != nil {
		log.Fatalf("session secret: %v", err)
	}
	sessions := auth.NewSessionManager(sessionSecret, secureCookies)
	mw := auth.NewMiddleware(sessions)
	mw.SetUserCheck(store.UserCheck)

	var provider auth.Provider
	switch cfg.AuthProvider {
	case "mock":
		mock := auth.NewMockProvider(cfg.MockUser, cfg.MockAdmin)
		provider = mock
		mw.SetBasicAuthenticator(mock)
		if err := store.UpsertExternalUser(mock.Username, mock.User().Email, mock.IsAdmin); err != nil {
			log.Fatalf("mock user: %v", err)
		}
		log.Printf("auth: mock provider (user=%q admin=%v)", cfg.MockUser, cfg.MockAdmin)
	case "plex":
		provider = auth.NewPlexProvider("Yomi Reader", cfg.PlexMachineID, cfg.PublicBaseURL, secureCookies)
		log.Printf("auth: plex provider (machine=%s)", cfg.PlexMachineID)
	default:
		local := auth.NewLocalProvider(store)
		provider = local
		mw.SetBasicAuthenticator(local)
		if err := bootstrapAdmin(cfg, store); err != nil {
			log.Fatalf("bootstrap admin: %v", err)
		}
		log.Printf("auth: local provider")
	}

	// --- Default library + initial scans ---
	bootstrapLibrary(cfg, store)
	if libs, err := store.ListLibraries(); err == nil {
		for _, lib := range libs {
			if lib.LastScan == nil {
				scanner.ScanAsync(lib, "initial scan")
			}
		}
	}
	if err := autoScan.Start(); err != nil {
		log.Printf("autoscan: disabled (%v)", err)
	}

	mux := server.NewMux(server.Deps{
		Provider:  provider,
		Sessions:  sessions,
		Mw:        mw,
		Library:   handler,
		StaticDir: cfg.StaticDir,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.LogRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("yomi-reader %s listening on :%s (data=%s)", version, cfg.Port, cfg.DataPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Printf("shutting down...")
	scanner.Shutdown()
	autoScan.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	covers.Stop()
	library.CloseArchives()
	log.Printf("shutdown complete")
}

// bootstrapAdmin creates the first admin account: from ADMIN_USER/ADMIN_PASS
// when provided, or admin/admin on an empty database.
func bootstrapAdmin(cfg *config.Config, store *library.Store) error {
	n, err := store.CountUsers()
	if err != nil {
		return err
	}
	username, password := cfg.AdminUser, cfg.AdminPass
	explicit := username != "" && password != ""

	if n > 0 && !explicit {
		return nil
	}
	if username == "" {
		username = "admin"
	}
	if password == "" {
		password = "admin"
	}
	if _, err := store.GetUser(username); err == nil {
		return nil // already exists; never overwrite an existing password
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u := &library.User{Username: username, IsAdmin: true, MaxAgeRating: library.RatingExplicit}
	if err := store.CreateUser(u, hash); err != nil {
		return err
	}
	if !explicit {
		log.Printf("WARNING: created default admin account %q with password %q — change it (or set ADMIN_USER/ADMIN_PASS)", username, password)
	} else {
		log.Printf("created admin account %q from ADMIN_USER/ADMIN_PASS", username)
	}
	return nil
}

// bootstrapLibrary registers DEFAULT_LIBRARY_PATH (or M:\books\suwayomi when
// present) as a library the first time the app runs.
func bootstrapLibrary(cfg *config.Config, store *library.Store) {
	if done, _ := store.GetSetting(library.SettingBootstrapDone, ""); done != "" {
		return
	}
	if libs, err := store.ListLibraries(); err == nil && len(libs) > 0 {
		_ = store.SetSetting(library.SettingBootstrapDone, "1")
		return
	}

	path := cfg.DefaultLibraryPath
	if path == "" {
		if st, err := os.Stat(bootstrapLibraryPath); err == nil && st.IsDir() {
			path = bootstrapLibraryPath
		}
	}
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		log.Printf("bootstrap: library path %q is not an accessible directory, skipping", abs)
		return
	}

	name := filepath.Base(abs)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "Library"
	}
	name = strings.ToUpper(name[:1]) + name[1:]
	lib, err := store.CreateLibrary(name, abs)
	if err != nil {
		log.Printf("bootstrap: create library: %v", err)
		return
	}
	_ = store.SetSetting(library.SettingBootstrapDone, "1")
	log.Printf("bootstrap: created library %q at %s", lib.Name, lib.RootPath)
}
