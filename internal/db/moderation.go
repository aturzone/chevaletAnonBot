package db

import "context"

// ModUser is one row of the admin moderation list.
type ModUser struct {
	UID     string
	Name    string
	Reports int
	Banned  bool
}

// GetReportedUsers lists everyone with at least one report, worst first.
//
// One query with a join rather than GetAllReports plus a GetName per uid: the list
// is rendered as a keyboard, so N round trips would be N per page for no reason.
func (db *DB) GetReportedUsers(ctx context.Context) ([]ModUser, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT r.reported_id,
		        COALESCE(u.name, ''),
		        count(*) AS c,
		        COALESCE(u.is_banned, FALSE)
		   FROM reports r
		   LEFT JOIN users u ON u.uid = r.reported_id
		  GROUP BY r.reported_id, u.name, u.is_banned
		  ORDER BY c DESC, r.reported_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanModUsers(rows)
}

// GetBannedUsers lists every banned user, with how many reports they carry (which
// may be zero — an admin can ban directly).
func (db *DB) GetBannedUsers(ctx context.Context) ([]ModUser, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT u.uid,
		        COALESCE(u.name, ''),
		        (SELECT count(*) FROM reports r WHERE r.reported_id = u.uid),
		        TRUE
		   FROM users u
		  WHERE u.is_banned
		  ORDER BY u.uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanModUsers(rows)
}

// GetModUser loads one user for the per-user action panel. A uid with reports but
// no users row still returns a usable entry, so a report can be cleared even when
// the account is gone.
func (db *DB) GetModUser(ctx context.Context, uid string) (ModUser, error) {
	m := ModUser{UID: uid}
	err := db.pool.QueryRow(ctx,
		`SELECT COALESCE((SELECT name FROM users WHERE uid=$1), ''),
		        (SELECT count(*) FROM reports WHERE reported_id=$1),
		        COALESCE((SELECT is_banned FROM users WHERE uid=$1), FALSE)`, uid).
		Scan(&m.Name, &m.Reports, &m.Banned)
	return m, err
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanModUsers(rows rowScanner) ([]ModUser, error) {
	var out []ModUser
	for rows.Next() {
		var m ModUser
		if err := rows.Scan(&m.UID, &m.Name, &m.Reports, &m.Banned); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
