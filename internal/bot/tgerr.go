package bot

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

// These predicates classify gotgbot API errors the way modules/Global/
// decorators.py classified PTB exceptions via str(e). They match the raw
// Telegram error descriptions case-insensitively (substring), which is
// equivalent to — and a little more robust than — the original exact-string
// checks.

func asTelegramError(err error) *gotgbot.TelegramError {
	var te *gotgbot.TelegramError
	if errors.As(err, &te) {
		return te
	}
	return nil
}

func descContains(err error, sub string) bool {
	te := asTelegramError(err)
	if te == nil {
		return false
	}
	return strings.Contains(strings.ToLower(te.Description), strings.ToLower(sub))
}

// errBotBlocked: "Forbidden: bot was blocked by the user".
func errBotBlocked(err error) bool { return descContains(err, "bot was blocked by the user") }

// errBotNotMember: "Forbidden: bot is not a member of the channel chat".
func errBotNotMember(err error) bool {
	return descContains(err, "bot is not a member of the channel chat")
}

// errReplyNotFound: "Bad Request: message to be replied not found".
func errReplyNotFound(err error) bool { return descContains(err, "message to be replied not found") }

// errMessageIDInvalid: "Bad Request: MESSAGE_ID_INVALID".
func errMessageIDInvalid(err error) bool { return descContains(err, "MESSAGE_ID_INVALID") }

// errQueryTooOld: "Bad Request: query is too old and response timeout expired …".
func errQueryTooOld(err error) bool { return descContains(err, "query is too old") }

// errMessageNotModified: "Bad Request: message is not modified: specified new
// message content and reply markup are exactly the same …". Harmless — it just
// means the user re-triggered a menu/state they were already on (e.g. a double
// tap, or pressing "back" while already at that menu), so the edit is a no-op
// Telegram refuses rather than a real failure.
func errMessageNotModified(err error) bool { return descContains(err, "message is not modified") }

// errForbidden matches any HTTP 403 "Forbidden: …" Telegram error, mirroring the
// PTB `except Forbidden` in is_reply_to_channel (e.g. a private channel the bot
// was never added to).
func errForbidden(err error) bool {
	te := asTelegramError(err)
	return te != nil && te.Code == 403
}

// isDBError reports whether err is a PostgreSQL/database failure, mirroring the
// Python prep's `except (psycopg2.Error, psycopg2.DatabaseError)`: a server-side
// SQL error (PgError) or a failure to establish a connection (ConnectError).
// Both warrant the user-facing "database problem" reply and an ERROR_CHAT_ID
// report. (A clean separation from isNetworkError: DB failures are pgconn types,
// never a *url.Error.)
func isDBError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return true
	}
	var connErr *pgconn.ConnectError
	return errors.As(err, &connErr)
}

// isNetworkError reports whether err is a transport-level failure talking to
// Telegram — not a Telegram API error (those carry a TelegramError) — mirroring
// the Python prep's `except NetworkError`. gotgbot speaks HTTP via net/http, so
// such failures surface as a *url.Error.
func isNetworkError(err error) bool {
	if asTelegramError(err) != nil {
		return false
	}
	var uerr *url.Error
	return errors.As(err, &uerr)
}

// errTooManyRequests matches a Telegram 429 flood wait, and returns how long
// Telegram asked us to wait.
//
// This is NOT a bug, and treating it as one made a flood worse: every 429 was
// filed as an incident, and each incident posts to ERROR_CHAT_ID and replies a
// tracking code to the user — so a rate limit produced two more sends per
// occurrence, on a bot that was already over its limit. 321 of them were logged in
// nine hours of normal traffic.
//
// Telegram reports the wait in TelegramError.ResponseParams.RetryAfter, and also
// in the description ("Too Many Requests: retry after 251") — the description is
// parsed as a fallback because the field is only populated for some errors.
func errTooManyRequests(err error) (retryAfter int64, ok bool) {
	te := asTelegramError(err)
	if te == nil {
		return 0, false
	}
	if te.Code != 429 && !strings.Contains(strings.ToLower(te.Description), "too many requests") {
		return 0, false
	}
	if te.ResponseParams != nil && te.ResponseParams.RetryAfter > 0 {
		return te.ResponseParams.RetryAfter, true
	}
	if i := strings.LastIndex(strings.ToLower(te.Description), "retry after "); i >= 0 {
		if n, cerr := strconv.ParseInt(strings.TrimSpace(te.Description[i+len("retry after "):]), 10, 64); cerr == nil {
			return n, true
		}
	}
	return 0, true
}

// floodSeconds returns the retry_after of a 429, or 0 if err is not one.
func floodSeconds(err error) int64 {
	n, ok := errTooManyRequests(err)
	if !ok {
		return 0
	}
	return n
}

// isFloodErr reports whether err is a 429 at all, including one Telegram sent
// without a retry_after value.
func isFloodErr(err error) bool {
	_, ok := errTooManyRequests(err)
	return ok
}

// errNoSendRights: "Bad Request: not enough rights to send text messages to the
// chat" (and the media variants).
//
// This is a FACT ABOUT A CHAT, not a fault. prep deliberately allows supergroups
// (Python parity — the bot answers commands and the catch-all there), so once
// somebody adds the bot to a group where members are restricted from posting, every
// message in that group makes the catch-all try to reply and be refused. Treating
// that as an incident filed one report per message into ERROR_CHAT_ID and tried to
// reply to the user with a tracking code — a reply that also could not be sent.
//
// Same family as errForbidden: nothing to fix, nothing to page anyone about.
func errNoSendRights(err error) bool {
	return descContains(err, "not enough rights to send")
}
