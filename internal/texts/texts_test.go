package texts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTextsLoaderTrimSubdirCacheAndMiss(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "greeting.txt"), []byte("  سلام دنیا \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a subdir template (name may contain a slash, mapped via FromSlash).
	if err := os.MkdirAll(filepath.Join(root, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings", "main.txt"), []byte("menu\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	l := New(root)

	// Get trims surrounding whitespace (== Python fetch_text .read().strip()).
	if got, err := l.Get("greeting"); err != nil || got != "سلام دنیا" {
		t.Errorf("Get(greeting) = (%q,%v); want (سلام دنیا,nil)", got, err)
	}
	if got, err := l.Get("settings/main"); err != nil || got != "menu" {
		t.Errorf("Get(settings/main) = (%q,%v); want (menu,nil)", got, err)
	}

	// a missing file errors AND is not cached: creating it later makes Get succeed.
	if _, err := l.Get("later"); err == nil {
		t.Fatal("Get(missing) should error")
	}
	if err := os.WriteFile(filepath.Join(root, "later.txt"), []byte("now here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := l.Get("later"); err != nil || got != "now here" {
		t.Errorf("Get(later) after creation = (%q,%v); want (now here,nil) — a miss must not be cached", got, err)
	}

	// caching is intentional: editing the file after a successful Get returns the
	// cached value (templates are mounted read-only in prod; documents the divergence
	// from Python's re-read-every-call).
	if err := os.WriteFile(filepath.Join(root, "greeting.txt"), []byte("CHANGED"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Get("greeting"); got != "سلام دنیا" {
		t.Errorf("Get(greeting) after edit = %q; want the cached سلام دنیا", got)
	}
}
