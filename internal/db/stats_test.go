package db

import (
	"context"
	"testing"
)

// TestStatsAndModeration runs the new admin queries against a real PostgreSQL.
//
// The migration behaviour is the important part: created_at is added WITHOUT a
// default so pre-existing rows stay NULL. If it were added with `DEFAULT now()`,
// every existing user would be stamped with the migration time and the first
// stats report would claim ~17k people joined today.
func TestStatsAndModeration(t *testing.T) {
	ctx := context.Background()
	d, err := Connect(ctx, testConfig(t))
	noErr(t, err, "Connect")
	defer d.Close()

	// Simulate the real upgrade path against a REALISTIC schema: build the current
	// one, then drop exactly the columns this change adds, so the second MakeTables
	// exercises the same migration production ran.
	_, err = d.pool.Exec(ctx, `DROP TABLE IF EXISTS users, cids, reports, blocks, report_cases, daily_metrics`)
	noErr(t, err, "drop")
	noErr(t, d.MakeTables(ctx), "MakeTables (baseline)")
	_, err = d.pool.Exec(ctx, `ALTER TABLE users DROP COLUMN created_at, DROP COLUMN last_active_at`)
	noErr(t, err, "strip the new columns")
	_, err = d.pool.Exec(ctx, `DROP TABLE daily_metrics`)
	noErr(t, err, "strip daily_metrics")

	// Two users that exist BEFORE the columns are added.
	_, err = d.pool.Exec(ctx, `INSERT INTO users (uid, name) VALUES ('old1','Old One'), ('old2','Old Two')`)
	noErr(t, err, "insert legacy users")

	noErr(t, d.MakeTables(ctx), "MakeTables (migration)")

	// The whole point: existing users must NOT look like today's signups.
	s, err := d.GetStats(ctx, 7)
	noErr(t, err, "GetStats")
	if s.Today.NewUsers != 0 {
		t.Errorf("NewUsers today = %d after migrating 2 pre-existing users; want 0", s.Today.NewUsers)
	}
	if s.UsersWithoutJoinDate != 2 {
		t.Errorf("UsersWithoutJoinDate = %d; want 2 (reported separately, not hidden)", s.UsersWithoutJoinDate)
	}
	if s.TotalUsers != 2 {
		t.Errorf("TotalUsers = %d; want 2", s.TotalUsers)
	}

	// A user added after the migration IS counted as new today.
	added, err := d.AddUser(ctx, "new1", "New One")
	noErr(t, err, "AddUser")
	if !added {
		t.Fatal("AddUser reported the user already existed")
	}
	s, err = d.GetStats(ctx, 7)
	noErr(t, err, "GetStats after AddUser")
	if s.Today.NewUsers != 1 {
		t.Errorf("NewUsers today = %d after one signup; want 1", s.Today.NewUsers)
	}

	// Activity: nobody is active until touched, then exactly the touched user is.
	if s.Today.Active != 0 {
		t.Errorf("Active today = %d before any touch; want 0", s.Today.Active)
	}
	noErr(t, d.TouchUser(ctx, "new1"), "TouchUser")
	// Touching twice must not double-count — it writes at most once per day.
	noErr(t, d.TouchUser(ctx, "new1"), "TouchUser (again)")
	s, err = d.GetStats(ctx, 7)
	noErr(t, err, "GetStats after touch")
	if s.Today.Active != 1 {
		t.Errorf("Active today = %d after touching one user twice; want 1", s.Today.Active)
	}

	// Message counter: starts at zero, accumulates, and stays one row per day.
	for i := 0; i < 5; i++ {
		noErr(t, d.CountMessage(ctx), "CountMessage")
	}
	s, err = d.GetStats(ctx, 7)
	noErr(t, err, "GetStats after counting")
	if s.Today.Messages != 5 {
		t.Errorf("Messages today = %d; want 5", s.Today.Messages)
	}
	var dayRows int
	noErr(t, d.pool.QueryRow(ctx, `SELECT count(*) FROM daily_metrics`).Scan(&dayRows), "count daily_metrics")
	if dayRows != 1 {
		t.Errorf("daily_metrics rows = %d after 5 messages in one day; want 1", dayRows)
	}

	// The history window returns exactly the requested number of past days.
	if len(s.Days) != 7 {
		t.Errorf("history days = %d; want 7", len(s.Days))
	}

	// ---- moderation lists ----
	noErr(t, d.pool.QueryRow(ctx, `SELECT 1`).Scan(new(int)), "ping")
	if _, err := d.AddReportID(ctx, "new1"); err != nil {
		t.Fatalf("AddReportID: %v", err)
	}
	if _, err := d.AddReportID(ctx, "new1"); err != nil {
		t.Fatalf("AddReportID: %v", err)
	}
	if _, err := d.AddReportID(ctx, "old1"); err != nil {
		t.Fatalf("AddReportID: %v", err)
	}

	reported, err := d.GetReportedUsers(ctx)
	noErr(t, err, "GetReportedUsers")
	if len(reported) != 2 {
		t.Fatalf("reported users = %d; want 2", len(reported))
	}
	// Worst offender first — that is the ordering an admin needs.
	if reported[0].UID != "new1" || reported[0].Reports != 2 {
		t.Errorf("first row = (%s,%d); want (new1,2) — highest count first",
			reported[0].UID, reported[0].Reports)
	}
	if reported[0].Name != "New One" {
		t.Errorf("name = %q; want New One (joined from users)", reported[0].Name)
	}
	if reported[0].Banned {
		t.Error("user reported as banned before any ban")
	}

	// Banning shows up in both the flag and the banned list.
	noErr(t, d.BanAction(ctx, "new1", true), "BanAction(ban)")
	banned, err := d.GetBannedUsers(ctx)
	noErr(t, err, "GetBannedUsers")
	if len(banned) != 1 || banned[0].UID != "new1" {
		t.Fatalf("banned users = %+v; want just new1", banned)
	}
	if banned[0].Reports != 2 {
		t.Errorf("banned user's report count = %d; want 2", banned[0].Reports)
	}

	one, err := d.GetModUser(ctx, "new1")
	noErr(t, err, "GetModUser")
	if !one.Banned || one.Reports != 2 || one.Name != "New One" {
		t.Errorf("GetModUser = %+v; want banned, 2 reports, New One", one)
	}

	// One-click unban is the headline feature: it must actually clear the flag and
	// empty the banned list.
	noErr(t, d.BanAction(ctx, "new1", false), "BanAction(unban)")
	banned, err = d.GetBannedUsers(ctx)
	noErr(t, err, "GetBannedUsers after unban")
	if len(banned) != 0 {
		t.Errorf("banned users after unban = %+v; want none", banned)
	}

	// Clearing reports removes them from the reported list entirely.
	n, err := d.DelReportID(ctx, "new1")
	noErr(t, err, "DelReportID")
	if n != 2 {
		t.Errorf("DelReportID returned %d; want 2", n)
	}
	reported, err = d.GetReportedUsers(ctx)
	noErr(t, err, "GetReportedUsers after clear")
	if len(reported) != 1 || reported[0].UID != "old1" {
		t.Errorf("reported after clearing new1 = %+v; want only old1", reported)
	}

	// A report against a uid with no users row must still be listed and clearable,
	// so a report can be handled after the account is gone.
	if _, err := d.AddReportID(ctx, "ghost"); err != nil {
		t.Fatalf("AddReportID(ghost): %v", err)
	}
	reported, err = d.GetReportedUsers(ctx)
	noErr(t, err, "GetReportedUsers with a ghost")
	found := false
	for _, r := range reported {
		if r.UID == "ghost" {
			found = true
			if r.Name != "" || r.Banned {
				t.Errorf("ghost row = %+v; want empty name and not banned", r)
			}
		}
	}
	if !found {
		t.Error("a report against an unknown uid vanished from the list")
	}
	ghost, err := d.GetModUser(ctx, "ghost")
	noErr(t, err, "GetModUser(ghost)")
	if ghost.Reports != 1 {
		t.Errorf("ghost reports = %d; want 1", ghost.Reports)
	}
}
