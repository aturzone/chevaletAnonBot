package db

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestReportCases exercises the report-case table against a real PostgreSQL.
// ClaimReportCase in particular is a single CTE doing check-and-set, and it is
// what stops two admins both banning or both counting the same report — too
// subtle to trust without running it.
func TestReportCases(t *testing.T) {
	ctx := context.Background()
	d, err := Connect(ctx, testConfig(t))
	noErr(t, err, "Connect")
	defer d.Close()
	noErr(t, d.MakeTables(ctx), "MakeTables")

	// Clean slate for the ids this test uses.
	_, _ = d.pool.Exec(ctx, `DELETE FROM report_cases WHERE report_id LIKE 'test-%'`)

	c := ReportCase{
		ReportID:      "test-case-1",
		ReporterID:    "111",
		ReportedID:    "222",
		ChannelChatID: -1002157529004,
		ChannelMsgID:  771,
	}
	noErr(t, d.AddReportCase(ctx, c), "AddReportCase")

	// Re-adding the same id must not error or duplicate (the report is already
	// posted; a retry must be harmless).
	noErr(t, d.AddReportCase(ctx, c), "AddReportCase (idempotent)")

	got, err := d.GetReportCase(ctx, "test-case-1")
	noErr(t, err, "GetReportCase")
	if got.ReporterID != "111" || got.ReportedID != "222" {
		t.Errorf("pair = (%s,%s); want (111,222)", got.ReporterID, got.ReportedID)
	}
	if got.ChannelChatID != -1002157529004 || got.ChannelMsgID != 771 {
		t.Errorf("channel ref = (%d,%d); want (-1002157529004,771)", got.ChannelChatID, got.ChannelMsgID)
	}
	if got.Handled {
		t.Error("a fresh case reports Handled=true")
	}

	// Unknown id -> a typed error, so the caller can say "not found" rather than
	// leaking a scan failure.
	if _, err := d.GetReportCase(ctx, "test-does-not-exist"); !errors.Is(err, ErrNoReportCase) {
		t.Errorf("GetReportCase(unknown) = %v; want ErrNoReportCase", err)
	}

	// First claim wins.
	won, holder, action, err := d.ClaimReportCase(ctx, "test-case-1", "ban", "@first")
	noErr(t, err, "ClaimReportCase (first)")
	if !won || holder != "@first" || action != "ban" {
		t.Errorf("first claim = (%v,%q,%q); want (true,@first,ban)", won, holder, action)
	}

	// Second claim loses AND reports who holds it, so the loser can be told.
	won, holder, action, err = d.ClaimReportCase(ctx, "test-case-1", "report", "@second")
	noErr(t, err, "ClaimReportCase (second)")
	if won {
		t.Error("second claim won; two admins could both act on one report")
	}
	if holder != "@first" || action != "ban" {
		t.Errorf("loser was told (%q,%q); want (@first,ban)", holder, action)
	}

	// The losing claim must not have overwritten the stored action.
	got, err = d.GetReportCase(ctx, "test-case-1")
	noErr(t, err, "GetReportCase after claims")
	if !got.Handled || got.HandledBy != "@first" || got.Action != "ban" {
		t.Errorf("stored state = (%v,%q,%q); want (true,@first,ban)", got.Handled, got.HandledBy, got.Action)
	}

	// Claiming an unknown case is a typed error, not a silent success.
	if _, _, _, err := d.ClaimReportCase(ctx, "test-nope", "ban", "@x"); !errors.Is(err, ErrNoReportCase) {
		t.Errorf("ClaimReportCase(unknown) = %v; want ErrNoReportCase", err)
	}

	// Concurrency: many admins tapping at once must produce EXACTLY one winner.
	// This is the property the whole design rests on.
	c2 := ReportCase{ReportID: "test-race", ReporterID: "1", ReportedID: "2"}
	noErr(t, d.AddReportCase(ctx, c2), "AddReportCase (race)")

	const n = 12
	var wg sync.WaitGroup
	wins := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, _, _, err := d.ClaimReportCase(context.Background(), "test-race", "ban", "@admin")
			if err != nil {
				t.Errorf("concurrent claim %d: %v", i, err)
				return
			}
			wins[i] = w
		}(i)
	}
	wg.Wait()

	total := 0
	for _, w := range wins {
		if w {
			total++
		}
	}
	if total != 1 {
		t.Errorf("%d concurrent claims won; want exactly 1", total)
	}

	_, _ = d.pool.Exec(ctx, `DELETE FROM report_cases WHERE report_id LIKE 'test-%'`)
}
