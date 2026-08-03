package bot

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// TestReportActionKeyboardShape locks the 4-button layout and, more importantly,
// that every callback_data fits Telegram's 64-byte cap. A token is 31 chars, so
// there is room — but a longer verb or an extra field would silently make
// Telegram reject the whole keyboard, and the report would post without buttons.
func TestReportActionKeyboardShape(t *testing.T) {
	tc := encoder.NewTokenCipher("test-bot-token")
	reporterTok, err := tc.Seal(1000000001, nil)
	if err != nil {
		t.Fatalf("Seal reporter: %v", err)
	}
	reportedTok, err := tc.Seal(1000000002, nil)
	if err != nil {
		t.Fatalf("Seal reported: %v", err)
	}

	kb := reportActionKeyboard(reporterTok, reportedTok)
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("rows = %d; want 2", len(kb.InlineKeyboard))
	}
	for i, r := range kb.InlineKeyboard {
		if len(r) != 2 {
			t.Fatalf("row%d buttons = %d; want 2", i, len(r))
		}
	}

	for _, r := range kb.InlineKeyboard {
		for _, btn := range r {
			if len(btn.CallbackData) > 64 {
				t.Errorf("callback_data %q is %d bytes (>64, Telegram rejects it)",
					btn.CallbackData, len(btn.CallbackData))
			}
			if !strings.HasPrefix(btn.CallbackData, "rpt|") {
				t.Errorf("callback_data %q lacks the rpt| prefix the handler matches", btn.CallbackData)
			}
			// No raw Telegram id may appear: that is the whole point of sealing.
			for _, raw := range []string{"1000000001", "1000000002"} {
				if strings.Contains(btn.CallbackData, raw) {
					t.Errorf("callback_data %q leaks a raw uid", btn.CallbackData)
				}
			}
		}
	}

	// accept and ban must act on the REPORTED user; the reporter must never be the
	// one banned by a mis-wired button.
	accept, ban := kb.InlineKeyboard[0][0], kb.InlineKeyboard[0][1]
	if accept.CallbackData != "rpt|"+rptVerbAccept+"|"+reportedTok {
		t.Errorf("accept targets %q; want the reported token", accept.CallbackData)
	}
	if ban.CallbackData != "rpt|"+rptVerbBan+"|"+reportedTok {
		t.Errorf("ban targets %q; want the reported token", ban.CallbackData)
	}
	msgReporter, msgReported := kb.InlineKeyboard[1][0], kb.InlineKeyboard[1][1]
	if msgReporter.CallbackData != "rpt|"+rptVerbMsgReporter+"|"+reporterTok {
		t.Errorf("message-reporter targets %q; want the reporter token", msgReporter.CallbackData)
	}
	if msgReported.CallbackData != "rpt|"+rptVerbMsgReported+"|"+reportedTok {
		t.Errorf("message-reported targets %q; want the reported token", msgReported.CallbackData)
	}
}

// TestRptMarkDoneReplacesOnlyActionRow proves the "who did it" swap kills the
// accept/ban row (so two admins cannot both ban) while leaving the two message
// buttons alive.
func TestRptMarkDoneReplacesOnlyActionRow(t *testing.T) {
	kb := reportActionKeyboard("TOKREPORTER", "TOKREPORTED")
	rows := kb.InlineKeyboard

	newRows := make([][]gotgbot.InlineKeyboardButton, 0, len(rows))
	for _, r := range rows {
		if rowHasVerb(r, rptVerbAccept) || rowHasVerb(r, rptVerbBan) {
			newRows = append(newRows, rptDoneRow("🚫 بن شد — @someadmin"))
			continue
		}
		newRows = append(newRows, r)
	}

	if len(newRows) != 2 {
		t.Fatalf("rows after mark-done = %d; want 2", len(newRows))
	}
	if len(newRows[0]) != 1 || newRows[0][0].CallbackData != "no-callback" {
		t.Errorf("action row was not replaced by a dead status button: %+v", newRows[0])
	}
	if !strings.Contains(newRows[0][0].Text, "@someadmin") {
		t.Errorf("status button %q does not name the admin who acted", newRows[0][0].Text)
	}
	// The message buttons must still work afterwards.
	if len(newRows[1]) != 2 {
		t.Fatalf("message row buttons = %d; want 2 (still usable)", len(newRows[1]))
	}
	for _, btn := range newRows[1] {
		if btn.CallbackData == "no-callback" {
			t.Errorf("message button %q was disabled; it should stay usable", btn.Text)
		}
	}
}

// TestRowHasVerb guards the row matcher against matching a verb that merely
// appears inside a token (a sealed token is arbitrary base64url text).
func TestRowHasVerb(t *testing.T) {
	r := row(cb("x", "rpt|a|TOKEN"), cb("y", "rpt|b|TOKEN"))
	if !rowHasVerb(r, rptVerbAccept) {
		t.Error("rowHasVerb(accept) = false; want true")
	}
	if !rowHasVerb(r, rptVerbBan) {
		t.Error("rowHasVerb(ban) = false; want true")
	}
	if rowHasVerb(r, rptVerbMsgReporter) {
		t.Error("rowHasVerb(msg-reporter) = true; want false")
	}
	// A token containing the verb letters must not count as that verb.
	r2 := row(cb("x", "rpt|"+rptVerbMsgReported+"|aXbXmrXmd"))
	if rowHasVerb(r2, rptVerbAccept) || rowHasVerb(r2, rptVerbBan) {
		t.Error("a verb-like substring inside the token was matched as a verb")
	}
	if !rowHasVerb(r2, rptVerbMsgReported) {
		t.Error("rowHasVerb did not match the real verb")
	}
}

// TestParseComposeRef covers recovering the target from the prompt the admin
// replies to. This is what makes the compose flow stateless: get it wrong and a
// reply either goes nowhere or, worse, to the wrong person.
func TestParseComposeRef(t *testing.T) {
	tok := "abcdefghijklmnopqrstuvwxyz01234"

	for _, tc := range []struct {
		name, prompt, wantRole, wantTok string
	}{
		{"reporter", "bla bla\n<code>" + composeMarker + "reporter:" + tok + "</code>", "reporter", tok},
		{"reported", "bla bla\n<code>" + composeMarker + "reported:" + tok + "</code>", "reported", tok},
		{"marker at end without tags", composeMarker + "reporter:" + tok, "reporter", tok},
		{"trailing text is not eaten", composeMarker + "reported:" + tok + " and more words", "reported", tok},
		{"trailing newline", composeMarker + "reporter:" + tok + "\nnext line", "reporter", tok},
	} {
		t.Run(tc.name, func(t *testing.T) {
			role, got, ok := parseComposeRef(tc.prompt)
			if !ok {
				t.Fatalf("parseComposeRef(%q) not ok", tc.prompt)
			}
			if role != tc.wantRole {
				t.Errorf("role = %q; want %q", role, tc.wantRole)
			}
			if got != tc.wantTok {
				t.Errorf("token = %q; want %q", got, tc.wantTok)
			}
		})
	}

	// Anything malformed must be refused rather than guessed at.
	for _, bad := range []string{
		"",
		"a normal message with no marker",
		composeMarker,                         // nothing after it
		composeMarker + "reporter",            // no token
		composeMarker + "reporter:",           // empty token
		composeMarker + "somebodyelse:" + tok, // unknown role
		composeMarker + ":" + tok,             // empty role
	} {
		if role, tk, ok := parseComposeRef(bad); ok {
			t.Errorf("parseComposeRef(%q) = (%q,%q,true); want not ok", bad, role, tk)
		}
	}
}

// TestComposePromptIsParseable ties the two halves together: the exact prompt the
// bot sends must be readable back by the parser. Building the prompt and parsing
// it live in different functions, so this is the seam that could drift.
func TestComposePromptIsParseable(t *testing.T) {
	tc := encoder.NewTokenCipher("test-bot-token")
	const uid int64 = 1000000009
	tok, err := tc.Seal(uid, nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, role := range []string{"reporter", "reported"} {
		who := "ریپورت‌کننده"
		if role == "reported" {
			who = "کاربر ریپورت‌شده"
		}
		// Mirrors rptAskCompose's prompt.
		prompt := "✉️ <b>ارسال پیام به " + who + "</b>\n\n" +
			"متن پیام رو در جواب همین پیام بنویس. پیام با اسم بات براش فرستاده میشه.\n" +
			"برای انصراف /cancel رو بفرست.\n\n" +
			"<code>" + composeMarker + role + ":" + tok + "</code>"

		gotRole, gotTok, ok := parseComposeRef(prompt)
		if !ok {
			t.Fatalf("role %s: the bot's own prompt did not parse", role)
		}
		if gotRole != role {
			t.Errorf("role = %q; want %q", gotRole, role)
		}
		// And the token must still open to the original uid.
		gotUID, ok := tc.Open(gotTok, nil)
		if !ok || gotUID != uid {
			t.Errorf("token round-trip = (%d,%v); want (%d,true)", gotUID, ok, uid)
		}
	}
}

// TestAdminLabel checks the "who did it" name, including the fallback when an
// admin has no username or first name.
func TestAdminLabel(t *testing.T) {
	if got := adminLabel(gotgbot.User{Id: 7, Username: "atur", FirstName: "Atur"}); got != "@atur" {
		t.Errorf("adminLabel with username = %q; want @atur", got)
	}
	if got := adminLabel(gotgbot.User{Id: 7, FirstName: "Atur"}); got != "Atur" {
		t.Errorf("adminLabel without username = %q; want Atur", got)
	}
	if got := adminLabel(gotgbot.User{Id: 7}); got != "7" {
		t.Errorf("adminLabel with neither = %q; want the id", got)
	}
}
