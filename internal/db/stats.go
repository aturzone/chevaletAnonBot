package db

import (
	"context"
	"time"
)

// DayStat is one day's activity.
type DayStat struct {
	Day      time.Time
	NewUsers int
	Active   int
	Messages int64
}

// Stats is the /admin_stats payload.
type Stats struct {
	TotalUsers   int
	TotalLinks   int
	TotalReports int
	TotalBanned  int
	// UsersWithoutJoinDate are rows that predate created_at tracking. Surfaced so
	// a "new users" figure can never be mistaken for the whole user base.
	UsersWithoutJoinDate int
	Today                DayStat
	Days                 []DayStat // most recent first, excluding today
}

// TouchUser records that a user was active, at most one write per user per day.
//
// The WHERE clause is the point: it matches only when the stored value is from
// before today, so the common case is an UPDATE that touches no row rather than a
// write on every single update the bot handles.
func (db *DB) TouchUser(ctx context.Context, uid string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET last_active_at = now()
		  WHERE uid = $1
		    AND (last_active_at IS NULL OR last_active_at < current_date)`, uid)
	return err
}

// CountMessage adds one to today's delivered-message counter.
//
// A counter, not a log: no sender, no recipient, no content. Volume can be
// reported without recording who messaged whom.
func (db *DB) CountMessage(ctx context.Context) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO daily_metrics (day, messages) VALUES (current_date, 1)
		 ON CONFLICT (day) DO UPDATE SET messages = daily_metrics.messages + 1`)
	return err
}

// GetStats gathers the totals, today, and the previous days days of history.
func (db *DB) GetStats(ctx context.Context, days int) (Stats, error) {
	var s Stats
	if days < 1 {
		days = 7
	}

	err := db.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM users),
		        (SELECT count(*) FROM cids),
		        (SELECT count(*) FROM reports),
		        (SELECT count(*) FROM users WHERE is_banned),
		        (SELECT count(*) FROM users WHERE created_at IS NULL)`).
		Scan(&s.TotalUsers, &s.TotalLinks, &s.TotalReports, &s.TotalBanned, &s.UsersWithoutJoinDate)
	if err != nil {
		return s, err
	}

	// COALESCE wraps the SUBQUERY, not the column: with no daily_metrics row yet
	// (any day before the first delivered message) an inner COALESCE never runs and
	// the subquery yields NULL, which would fail the scan and break /admin_stats
	// outright.
	err = db.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM users WHERE created_at >= current_date),
		        (SELECT count(*) FROM users WHERE last_active_at >= current_date),
		        COALESCE((SELECT messages FROM daily_metrics WHERE day = current_date), 0)`).
		Scan(&s.Today.NewUsers, &s.Today.Active, &s.Today.Messages)
	if err != nil {
		return s, err
	}
	s.Today.Day = time.Now()

	// One row per past day. Active counts come from last_active_at, which only ever
	// holds the LATEST day a user was seen — so a past day's "active" is the number
	// of users whose last activity was that day, not everyone who used the bot then.
	// Messages are exact; that distinction is spelled out in the reply.
	rows, err := db.pool.Query(ctx,
		`SELECT d::date,
		        (SELECT count(*) FROM users WHERE created_at::date = d::date),
		        (SELECT count(*) FROM users WHERE last_active_at::date = d::date),
		        COALESCE((SELECT messages FROM daily_metrics WHERE day = d::date), 0)
		   FROM generate_series(current_date - $1::int, current_date - 1, interval '1 day') AS d
		  ORDER BY d DESC`, days)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var ds DayStat
		if err := rows.Scan(&ds.Day, &ds.NewUsers, &ds.Active, &ds.Messages); err != nil {
			return s, err
		}
		s.Days = append(s.Days, ds)
	}
	return s, rows.Err()
}
