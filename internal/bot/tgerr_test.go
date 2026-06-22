package bot

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestErrorPredicates locks the error classification handleErr depends on to
// reproduce the Python prep's except blocks (Forbidden / psycopg2.Error /
// NetworkError / benign BadRequests).
func TestErrorPredicates(t *testing.T) {
	forbidden := &gotgbot.TelegramError{Code: 403, Description: "Forbidden: bot was blocked by the user"}
	queryOld := &gotgbot.TelegramError{Code: 400, Description: "Bad Request: query is too old and response timeout expired"}
	replyGone := &gotgbot.TelegramError{Code: 400, Description: "Bad Request: message to be replied not found"}
	pgErr := &pgconn.PgError{Code: "57014"}
	netErr := &url.Error{Op: "Post", URL: "https://api.telegram.org", Err: errors.New("dial tcp: timeout")}
	plain := errors.New("boom")

	// errForbidden: only a 403 TelegramError.
	if !errForbidden(forbidden) {
		t.Error("errForbidden(403) should be true")
	}
	if errForbidden(queryOld) || errForbidden(netErr) || errForbidden(plain) {
		t.Error("errForbidden should be false for non-403")
	}

	// descContains-based predicates (case-insensitive substring).
	if !errQueryTooOld(queryOld) {
		t.Error("errQueryTooOld should match")
	}
	if !errReplyNotFound(replyGone) {
		t.Error("errReplyNotFound should match")
	}
	if errQueryTooOld(netErr) || errReplyNotFound(plain) {
		t.Error("desc predicates should be false for non-Telegram errors")
	}

	// isDBError: a PgError (and a wrapped one); never a url.Error/TelegramError.
	if !isDBError(pgErr) {
		t.Error("isDBError(PgError) should be true")
	}
	if !isDBError(fmt.Errorf("query failed: %w", pgErr)) {
		t.Error("isDBError should see through a wrap")
	}
	if isDBError(netErr) || isDBError(forbidden) || isDBError(plain) {
		t.Error("isDBError should be false for network/telegram/plain errors")
	}

	// isNetworkError: a url.Error (and wrapped); never a TelegramError.
	if !isNetworkError(netErr) {
		t.Error("isNetworkError(url.Error) should be true")
	}
	if !isNetworkError(fmt.Errorf("send: %w", netErr)) {
		t.Error("isNetworkError should see through a wrap")
	}
	if isNetworkError(forbidden) || isNetworkError(pgErr) || isNetworkError(plain) {
		t.Error("isNetworkError should be false for telegram/db/plain errors")
	}
}
