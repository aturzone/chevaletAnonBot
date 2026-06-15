package db

import "context"

// AddReportID inserts a report row and returns the new occurrence count for that
// id. Mirrors DBHandler.add_report_id.
func (db *DB) AddReportID(ctx context.Context, reportID string) (int, error) {
	if _, err := db.pool.Exec(ctx,
		`INSERT INTO reports (reported_id) VALUES ($1)`, reportID); err != nil {
		return 0, err
	}
	return db.GetReportID(ctx, reportID)
}

// DelReportID deletes every occurrence of a report id and returns how many there
// were. Mirrors DBHandler.del_report_id.
func (db *DB) DelReportID(ctx context.Context, reportID string) (int, error) {
	count, err := db.GetReportID(ctx, reportID)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		if _, err := db.pool.Exec(ctx,
			`DELETE FROM reports WHERE reported_id=$1`, reportID); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// GetReportID mirrors DBHandler.get_report_id.
func (db *DB) GetReportID(ctx context.Context, reportID string) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reports WHERE reported_id=$1`, reportID).Scan(&n)
	return n, err
}

// GetAllReports returns reported_id -> occurrence count. Mirrors
// DBHandler.get_all_reports (aggregated in SQL rather than in app code).
func (db *DB) GetAllReports(ctx context.Context) (map[string]int, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT reported_id, COUNT(*) FROM reports GROUP BY reported_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var id string
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
