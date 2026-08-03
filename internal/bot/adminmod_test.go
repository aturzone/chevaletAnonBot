package bot

import (
	"strings"
	"testing"

	"github.com/aturzone/chevaletAnonBot/internal/db"
)

// TestModUserLabel covers the button caption an admin scans down the list.
func TestModUserLabel(t *testing.T) {
	if got := modUserLabel(db.ModUser{UID: "123", Name: "Atur", Reports: 3}); got != "Atur — 3 ریپورت" {
		t.Errorf("label = %q; want the name and count", got)
	}

	// A banned user must be obvious at a glance without opening the panel.
	got := modUserLabel(db.ModUser{UID: "123", Name: "Atur", Reports: 3, Banned: true})
	if !strings.HasPrefix(got, "🚫 ") {
		t.Errorf("banned label = %q; want a 🚫 marker", got)
	}

	// No name (a report against an account with no users row): fall back to the uid
	// rather than rendering a blank, unidentifiable button.
	if got := modUserLabel(db.ModUser{UID: "999", Reports: 1}); !strings.HasPrefix(got, "999") {
		t.Errorf("label with no name = %q; want the uid as the fallback", got)
	}

	// A very long name must not push the counts out of the visible caption, which is
	// the part being scanned.
	long := strings.Repeat("ب", 200)
	got = modUserLabel(db.ModUser{UID: "1", Name: long, Reports: 7})
	if !strings.Contains(got, "7 ریپورت") {
		t.Errorf("long-name label dropped the count: %q", got)
	}
	if len(got) > 80 {
		t.Errorf("label is %d bytes; too long for a button caption", len(got))
	}
}

// TestFormatStats checks the dashboard states its own limits, so a number is never
// read as more than it is.
func TestFormatStats(t *testing.T) {
	out := formatStats(db.Stats{
		TotalUsers: 17250, TotalLinks: 17406, TotalReports: 15, TotalBanned: 2,
		UsersWithoutJoinDate: 17170,
		Today:                db.DayStat{NewUsers: 4, Active: 31, Messages: 128},
	})

	for _, want := range []string{"17250", "17406", "128", "31", "4"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats output is missing %q", want)
		}
	}
	// The caveats matter more than the figures: without them "4 new users" against
	// 17k existing rows invites the wrong conclusion.
	if !strings.Contains(out, "17170") {
		t.Error("output does not disclose how many users predate join-date tracking")
	}
	if !strings.Contains(out, "دقیقه") {
		t.Error("output does not explain that past-day 'active' differs from today's")
	}
}

// TestFormatStatsEmpty guards the first-run case: a brand-new deployment with no
// activity at all must still render, not divide by zero or hide sections oddly.
func TestFormatStatsEmpty(t *testing.T) {
	out := formatStats(db.Stats{})
	if out == "" {
		t.Fatal("empty stats produced no output")
	}
	if !strings.Contains(out, "امروز") {
		t.Error("empty stats output lost the today section")
	}
	// With nothing to disclose, the join-date caveat must not appear at all.
	if strings.Contains(out, "ثبت نشده") {
		t.Error("the join-date caveat appeared even though no users lack a join date")
	}
}
