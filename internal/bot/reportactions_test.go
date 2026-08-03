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

// TestParseReportIDFromSummary covers recovering the report code from the summary
// the admin acts on. Get this wrong and an action is applied without a claim, so
// two admins could both ban or both count the same report.
func TestParseReportIDFromSummary(t *testing.T) {
	// Exactly the shape handleReportCase builds, HTML already stripped by Telegram.
	const id = "hFdk4Tg3NY3jt9uEnxFUpq"
	summary := "⚙️ بررسی ریپورت\nکد: " + id + "\n\nریپورت‌کننده: u1 | @a\nریپورت‌شده: u2 | @b"
	if got := parseReportIDFromSummary(&gotgbot.Message{Text: summary}); got != id {
		t.Errorf("parseReportIDFromSummary = %q; want %q", got, id)
	}

	// With the stamp appended after an action, the code must still be found.
	stamped := summary + "\n\n🚫 بن شد — @atur"
	if got := parseReportIDFromSummary(&gotgbot.Message{Text: stamped}); got != id {
		t.Errorf("after stamping, got %q; want %q", got, id)
	}

	// No code, or no message: empty rather than garbage. An empty id downgrades to
	// "no claim", which still acts — better than refusing a real moderation action.
	for _, m := range []*gotgbot.Message{
		nil,
		{Text: ""},
		{Text: "an unrelated message"},
	} {
		if got := parseReportIDFromSummary(m); got != "" {
			t.Errorf("parseReportIDFromSummary(%v) = %q; want empty", m, got)
		}
	}
}

// TestReportDeepLink locks the /start payload: the report channel's link button is
// the ONLY way into the actions now, so the prefix and the id charset must stay
// deep-link safe and inside Telegram's 64-char limit.
func TestReportDeepLink(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := encoder.GenerateCID(20)
		payload := reportDeepLinkPrefix + id
		if len(payload) > 64 {
			t.Fatalf("deep-link payload %q is %d chars (>64)", payload, len(payload))
		}
		for j := 0; j < len(payload); j++ {
			c := payload[j]
			ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
				(c >= '0' && c <= '9') || c == '_' || c == '-'
			if !ok {
				t.Fatalf("payload %q has character %q, which Telegram rejects in a /start payload", payload, c)
			}
		}
		// startCmd routes on this prefix; it must not collide with the other one.
		if strings.HasPrefix(payload, "UNBLOCK-") {
			t.Fatalf("payload %q collides with the UNBLOCK- deep link", payload)
		}
		if strings.TrimPrefix(payload, reportDeepLinkPrefix) != id {
			t.Fatalf("round-trip of %q lost the id", payload)
		}
	}
}
