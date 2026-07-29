package bot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLatestBackupFile locks the selection rules: newest by MTIME (not name),
// directories skipped, and the min-age guard that keeps the nightly job from
// grabbing an hourly dump still being written.
func TestLatestBackupFile(t *testing.T) {
	dir := t.TempDir()

	// empty dir -> "" (the /admin backup "no backup yet" path)
	if got, err := latestBackupFile(dir, 0); err != nil || got != "" {
		t.Fatalf("empty dir: got (%q,%v); want (\"\",nil)", got, err)
	}

	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	mk("backup_mydatabase_20260708_140001.sql.gz", 10*time.Minute) // newest SETTLED
	mk("backup_mydatabase_20260708_130001.sql.gz", 2*time.Hour)
	mk("zzz-predeploy-20260706.sql.gz", 48*time.Hour) // lexicographically last but OLDEST

	// a subdirectory (even with a brand-new mtime) must never be returned.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// minAge 0: newest by mtime, ignoring the stale lexicographically-last file
	// and the subdir.
	got, err := latestBackupFile(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "backup_mydatabase_20260708_140001.sql.gz" {
		t.Errorf("latestBackupFile(0) = %q; want the newest-by-mtime file (not the stale lex-last one or a subdir)", got)
	}

	// min-age guard: a just-written (in-progress) dump must be skipped, so the
	// newest SETTLED file is returned instead.
	mk("backup_mydatabase_20260708_150001.sql.gz", 0) // written "now" — too fresh
	got, err = latestBackupFile(dir, 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got != "backup_mydatabase_20260708_140001.sql.gz" {
		t.Errorf("latestBackupFile(3m) = %q; want the newest SETTLED file, skipping the just-written one", got)
	}

	// missing dir -> error surfaces
	if _, err := latestBackupFile(filepath.Join(dir, "does-not-exist"), 0); err == nil {
		t.Error("missing dir: want an error, got nil")
	}
}

// TestTouchNightlySentMarker locks the marker file's name and its
// create-or-refresh behavior: deploy/go/nightly-backup-backstop.sh dedupes by
// checking this file's mtime, so a stale marker must never linger unrefreshed
// and a repeat call must always bump the mtime forward (not just "create once").
func TestTouchNightlySentMarker(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, nightlySentMarker)

	if err := touchNightlySentMarker(dir); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	info, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("marker not created: %v", err)
	}
	firstMtime := info.ModTime()

	// Back-date the marker (as if it were written last night), then touch again:
	// the mtime must move forward to "now", not stay pinned at the old value.
	old := firstMtime.Add(-24 * time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	if err := touchNightlySentMarker(dir); err != nil {
		t.Fatalf("second touch: %v", err)
	}
	info, err = os.Stat(marker)
	if err != nil {
		t.Fatalf("marker missing after second touch: %v", err)
	}
	if !info.ModTime().After(old) {
		t.Errorf("second touch did not refresh mtime: got %v, want after %v", info.ModTime(), old)
	}
}
