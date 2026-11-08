// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	DataPath      string
	Port          string
	SessionSecret string

	// AuthProvider selects the authentication backend: "local" or "mock".
	AuthProvider string

	// CookieSecure sets the Secure flag on the session cookie. Keep it false
	// when served over plain HTTP, browsers drop Secure cookies on http://.
	CookieSecure bool

	// Mock provider (AuthProvider == "mock").
	MockUser  string
	MockAdmin bool

	// Local provider bootstrap: first admin account.
	AdminUser string
	AdminPass string

	// ThumbWidth is the width in pixels of generated cover thumbnails.
	ThumbWidth int

	// DefaultLibraryPath, when set, is auto-registered as a library on first run.
	DefaultLibraryPath string

	// StaticDir is the directory holding the SPA (index.html, ...).
	StaticDir string

	// AutoScanWatch enables the filesystem watcher that triggers rescans.
	AutoScanWatch bool
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		DataPath:           getEnv("DATA_PATH", "./data"),
		Port:               getEnv("PORT", "8098"),
		SessionSecret:      os.Getenv("SESSION_SECRET"),
		AuthProvider:       strings.ToLower(getEnv("AUTH_PROVIDER", "local")),
		CookieSecure:       getEnvBool("COOKIE_SECURE", false),
		MockUser:           getEnv("MOCK_USER", "dev"),
		MockAdmin:          getEnvBool("MOCK_ADMIN", true),
		AdminUser:          strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPass:          os.Getenv("ADMIN_PASS"),
		DefaultLibraryPath: strings.TrimSpace(os.Getenv("DEFAULT_LIBRARY_PATH")),
		StaticDir:          getEnv("STATIC_DIR", "static"),
		AutoScanWatch:      getEnvBool("AUTOSCAN_WATCH", true),
	}

	width, err := strconv.Atoi(getEnv("THUMB_WIDTH", "300"))
	if err != nil || width < 50 || width > 2000 {
		return nil, fmt.Errorf("invalid THUMB_WIDTH: %q (expected 50..2000)", os.Getenv("THUMB_WIDTH"))
	}
	c.ThumbWidth = width

	if c.AuthProvider != "local" && c.AuthProvider != "mock" {
		return nil, fmt.Errorf("invalid AUTH_PROVIDER %q (expected \"local\" or \"mock\")", c.AuthProvider)
	}
	if c.DataPath == "" {
		return nil, fmt.Errorf("DATA_PATH is required")
	}
	if abs, err := filepath.Abs(c.DataPath); err == nil {
		c.DataPath = abs
	}
	return c, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
