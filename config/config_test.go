package config

import "testing"

func TestLoadDefaultIsPlex(t *testing.T) {
	t.Setenv("AUTH_PROVIDER", "")
	t.Setenv("PLEX_MACHINE_ID", "abc123")
	t.Setenv("DATA_PATH", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthProvider != "plex" {
		t.Fatalf("default provider = %q, want plex", cfg.AuthProvider)
	}
}

func TestLoadPlexRequiresMachineID(t *testing.T) {
	t.Setenv("AUTH_PROVIDER", "plex")
	t.Setenv("PLEX_MACHINE_ID", "")
	t.Setenv("DATA_PATH", t.TempDir())
	if _, err := Load(); err == nil {
		t.Fatal("expected error when PLEX_MACHINE_ID is empty")
	}

	t.Setenv("PLEX_MACHINE_ID", "abc123")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthProvider != "plex" || cfg.PlexMachineID != "abc123" {
		t.Fatalf("got provider=%q machine=%q", cfg.AuthProvider, cfg.PlexMachineID)
	}
}

func TestLoadRejectsUnknownProvider(t *testing.T) {
	t.Setenv("AUTH_PROVIDER", "carrier-pigeon")
	t.Setenv("DATA_PATH", t.TempDir())
	if _, err := Load(); err == nil {
		t.Fatal("expected error for unknown AUTH_PROVIDER")
	}
}
