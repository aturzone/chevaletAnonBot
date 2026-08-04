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
	notModified := &gotgbot.TelegramError{Code: 400, Description: "Bad Request: message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message"}
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
	if !errMessageNotModified(notModified) {
		t.Error("errMessageNotModified should match")
	}
	if errQueryTooOld(netErr) || errReplyNotFound(plain) || errMessageNotModified(plain) {
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

// TestErrNoSendRightsIsBenign guards the classification that stops one restricted
// group filling the error channel. Somebody adds the bot to a supergroup where
// members cannot post; prep allows supergroups, so every message there makes the
// catch-all try to reply and be refused. Filed as incidents that was one report per
// message — plus a tracking-code reply that also could not be sent.
func TestErrNoSendRightsIsBenign(t *testing.T) {
	err := &gotgbot.TelegramError{
		Method:      "sendMessage",
		Code:        400,
		Description: "Bad Request: not enough rights to send text messages to the chat",
	}
	if !errNoSendRights(err) {
		t.Error("the real production error was not classified as a missing-rights error")
	}
	// It must not be mistaken for anything that IS actionable.
	if errForbidden(err) {
		t.Error("a 400 was classified as a 403")
	}
	if _, isFlood := errTooManyRequests(err); isFlood {
		t.Error("a rights error was classified as a rate limit")
	}
	// Unrelated errors must not match, or real failures would be swallowed.
	for _, other := range []error{
		&gotgbot.TelegramError{Code: 400, Description: "Bad Request: message to copy not found"},
		&gotgbot.TelegramError{Code: 403, Description: "Forbidden: bot was blocked by the user"},
		&gotgbot.TelegramError{Code: 429, Description: "Too Many Requests: retry after 5"},
	} {
		if errNoSendRights(other) {
			t.Errorf("%v was wrongly classified as a missing-rights error", other)
		}
	}
}
