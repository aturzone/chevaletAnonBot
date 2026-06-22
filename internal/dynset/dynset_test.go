package dynset

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDynsetPersistResetAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic_settings.json")

	// no file yet -> config defaults.
	s := New(path, "http://default-url", "default-session")
	if got := s.AIURL(); got != "http://default-url" {
		t.Errorf("AIURL default = %q; want http://default-url", got)
	}
	if got := s.AISessionID(); got != "default-session" {
		t.Errorf("AISessionID default = %q; want default-session", got)
	}

	// set -> persists and overrides the default.
	s.SetAIURL("http://override")
	s.SetAISessionID("sess-123")
	if got := s.AIURL(); got != "http://override" {
		t.Errorf("AIURL after set = %q; want http://override", got)
	}

	// a fresh loader on the same path reads back the persisted overrides.
	s2 := New(path, "http://default-url", "default-session")
	if got := s2.AIURL(); got != "http://override" {
		t.Errorf("AIURL after reload = %q; want http://override (persisted)", got)
	}
	if got := s2.AISessionID(); got != "sess-123" {
		t.Errorf("AISessionID after reload = %q; want sess-123", got)
	}

	// reset -> reverts to the default and persists the removal.
	s2.ResetAIURL()
	if got := s2.AIURL(); got != "http://default-url" {
		t.Errorf("AIURL after reset = %q; want the default", got)
	}
	s3 := New(path, "http://default-url", "default-session")
	if got := s3.AIURL(); got != "http://default-url" {
		t.Errorf("AIURL after reset+reload = %q; want the default", got)
	}
	if got := s3.AISessionID(); got != "sess-123" {
		t.Errorf("AISessionID should still be persisted after only the URL was reset; got %q", got)
	}

	// file is written 0600 (hardening over the Python default umask). os perms are
	// a Unix concept — skip the bit check on Windows (the server/CI run verifies it).
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("settings file mode = %o; want 600", perm)
		}
	}
}

func TestDynsetMalformedFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic_settings.json")
	if err := os.WriteFile(path, []byte("{ not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a malformed file must not crash New; it falls back to the config defaults.
	s := New(path, "http://default-url", "default-session")
	if got := s.AIURL(); got != "http://default-url" {
		t.Errorf("AIURL with malformed file = %q; want the default", got)
	}
}
