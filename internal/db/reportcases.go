package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ReportCase is one report and how it was handled.
type ReportCase struct {
	ReportID      string
	ReporterID    string
	ReportedID    string
	ChannelChatID int64
	ChannelMsgID  int64
	Action        string // "" until an admin acts; then "report" or "ban"
	HandledBy     string
	Handled       bool
}

// ErrNoReportCase is returned when a report id is unknown (an old report from
// before this table existed, or a mangled deep link).
var ErrNoReportCase = errors.New("db: no such report case")

// AddReportCase records a new report and the channel message that announced it,
// so an admin can act on it later from a private chat.
func (db *DB) AddReportCase(ctx context.Context, c ReportCase) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO report_cases (report_id, reporter_id, reported_id, channel_chat_id, channel_msg_id)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (report_id) DO NOTHING`,
		c.ReportID, c.ReporterID, c.ReportedID, c.ChannelChatID, c.ChannelMsgID)
	return err
}

// GetReportCase loads a report case by its id.
func (db *DB) GetReportCase(ctx context.Context, reportID string) (ReportCase, error) {
	var c ReportCase
	var chatID, msgID *int64
	var action, handledBy *string
	err := db.pool.QueryRow(ctx,
		`SELECT report_id, reporter_id, reported_id, channel_chat_id, channel_msg_id,
		        action, handled_by, handled_at IS NOT NULL
		   FROM report_cases WHERE report_id=$1`, reportID).
		Scan(&c.ReportID, &c.ReporterID, &c.ReportedID, &chatID, &msgID,
			&action, &handledBy, &c.Handled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportCase{}, ErrNoReportCase
	}
	if err != nil {
		return ReportCase{}, err
	}
	if chatID != nil {
		c.ChannelChatID = *chatID
	}
	if msgID != nil {
		c.ChannelMsgID = *msgID
	}
	if action != nil {
		c.Action = *action
	}
	if handledBy != nil {
		c.HandledBy = *handledBy
	}
	return c, nil
}

// ClaimReportCase marks a case handled, but ONLY if it is not already. It reports
// whether this caller won the claim, plus who holds it otherwise.
//
// The check and the write are one statement so two admins acting at the same
// moment cannot both succeed — the loser is told who got there first instead of
// silently double-banning or double-counting the same report.
func (db *DB) ClaimReportCase(ctx context.Context, reportID, action, adminID string) (won bool, holder, heldAction string, err error) {
	// One statement does the whole check-and-set: the WHERE makes the row visible
	// to exactly one concurrent UPDATE, so only one caller gets a RETURNING row.
	err = db.pool.QueryRow(ctx,
		`UPDATE report_cases
		    SET action=$2, handled_by=$3, handled_at=now()
		  WHERE report_id=$1 AND handled_at IS NULL
		  RETURNING handled_by, action`, reportID, action, adminID).Scan(&holder, &heldAction)
	if err == nil {
		return true, holder, heldAction, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, "", "", err
	}

	// No row updated: either someone already holds it, or the id is unknown. Read
	// the holder in a SEPARATE statement rather than folding this into a CTE with
	// the UPDATE — a CTE shares one snapshot, so in a real race the loser read the
	// row as it was BEFORE the winner's write and got NULLs back. COALESCE still
	// guards the window where the winner has not committed yet.
	err = db.pool.QueryRow(ctx,
		`SELECT COALESCE(handled_by,''), COALESCE(action,'')
		   FROM report_cases WHERE report_id=$1`, reportID).Scan(&holder, &heldAction)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", "", ErrNoReportCase
	}
	if err != nil {
		return false, "", "", err
	}
	if holder == "" {
		// Lost the race but the winner is not committed yet. Say so honestly rather
		// than naming nobody.
		holder = "یکی از ادمین‌ها"
	}
	return false, holder, heldAction, nil
}
