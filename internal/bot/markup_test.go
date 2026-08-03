package bot

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// TestMessageKeyboard locks the callback_data format of the buttons under every
// delivered anonymous message. These strings are a frozen compatibility contract
// (historical callback_data the handlers parse), so any drift is a parity break.
func TestMessageKeyboard(t *testing.T) {
	// tokenMid carries the id-acting verbs (answer/seen/report); tokenBlock carries
	// block. Distinct values so the test proves each verb gets the RIGHT token.
	const tokenMid = "TOKMID"
	const tokenBlock = "TOKBLK"
	const mid int64 = 12345

	// ONE row now: [answer] [سایر]. Report and block moved behind the toggle, and
	// the blank spacer and donation row are gone. The caller appends the "sent with
	// link N" row, so a delivered message shows two rows in total.
	kb := messageKeyboard(tokenMid, tokenBlock, mid, false)
	if len(kb) != 1 {
		t.Fatalf("rows = %d; want 1 (the caller adds the link row)", len(kb))
	}
	if len(kb[0]) != 2 {
		t.Fatalf("row0 buttons = %d; want 2 (answer, سایر)", len(kb[0]))
	}
	if kb[0][0].CallbackData != "answer|TOKMID|12345" {
		t.Errorf("answer data = %q; want answer|TOKMID|12345", kb[0][0].CallbackData)
	}
	// The toggle carries tokenBlock, which is what makes expanding stateless: the
	// answer button supplies tokenMid+mid, this one supplies the block token.
	if kb[0][1].CallbackData != "oth|TOKBLK" {
		t.Errorf("سایر data = %q; want oth|TOKBLK", kb[0][1].CallbackData)
	}
	// The removed spacer must not come back.
	for i, btn := range kb[0] {
		if btn.CallbackData == "no-callback" {
			t.Errorf("row0[%d] is the removed blank spacer", i)
		}
	}
	// Report and block must NOT be on the visible keyboard any more.
	for _, r := range kb {
		for _, btn := range r {
			if strings.HasPrefix(btn.CallbackData, "report|") || strings.HasPrefix(btn.CallbackData, "block|") {
				t.Errorf("%q is still visible; it belongs behind سایر", btn.CallbackData)
			}
		}
	}

	// with "seen": the seen button is inserted at the FRONT of row0, ahead of both.
	kbs := messageKeyboard(tokenMid, tokenBlock, mid, true)
	if len(kbs[0]) != 3 {
		t.Fatalf("row0 buttons (seen on) = %d; want 3", len(kbs[0]))
	}
	if kbs[0][0].CallbackData != "seen|TOKMID|12345" {
		t.Errorf("seen data = %q; want seen|TOKMID|12345 (must be first)", kbs[0][0].CallbackData)
	}
	if kbs[0][2].CallbackData != "oth|TOKBLK" {
		t.Errorf("سایر must stay last; got %q", kbs[0][2].CallbackData)
	}
}

// TestOtherActionsRow covers what سایر reveals, including that the block button
// reflects the CURRENT block state rather than a value remembered in the data.
func TestOtherActionsRow(t *testing.T) {
	r := otherActionsRow("TOKMID", "TOKBLK", "12345", false)
	if len(r) != 2 {
		t.Fatalf("revealed buttons = %d; want 2", len(r))
	}
	if r[0].CallbackData != "report|TOKMID|12345" {
		t.Errorf("report data = %q", r[0].CallbackData)
	}
	if r[1].CallbackData != "block|TOKBLK" {
		t.Errorf("block data = %q; want block|TOKBLK", r[1].CallbackData)
	}
	// Already blocked -> the unblock action, so collapsing and reopening cannot
	// offer "block" to someone who has already blocked.
	rb := otherActionsRow("TOKMID", "TOKBLK", "12345", true)
	if rb[1].CallbackData != "unblock|TOKBLK" {
		t.Errorf("blocked state gave %q; want unblock|TOKBLK", rb[1].CallbackData)
	}
	if !isOtherActionsRow(r) || !isOtherActionsRow(rb) {
		t.Error("isOtherActionsRow does not recognise the row it must remove on collapse")
	}
	if isOtherActionsRow(messageKeyboard("A", "B", 1, true)[0]) {
		t.Error("isOtherActionsRow matched the main row; collapsing would delete the answer button")
	}
}

// TestAnswerTokenFromKeyboard is the reason the answer button is never hidden: it
// is where tokenMid and the message id are read back from on expand.
func TestAnswerTokenFromKeyboard(t *testing.T) {
	kb := messageKeyboard("TOKMID", "TOKBLK", 999, true)
	tok, midStr, ok := answerTokenFromKeyboard(kb)
	if !ok || tok != "TOKMID" || midStr != "999" {
		t.Errorf("got (%q,%q,%v); want (TOKMID,999,true)", tok, midStr, ok)
	}

	// A keyboard with no answer button must report failure rather than guess — that
	// path shows the user "this message is too old" instead of half a menu.
	if _, _, ok := answerTokenFromKeyboard([][]gotgbot.InlineKeyboardButton{
		{cb("x", "no-callback")},
	}); ok {
		t.Error("answerTokenFromKeyboard invented a token from a keyboard without one")
	}
	// Malformed data must not parse either.
	if _, _, ok := answerTokenFromKeyboard([][]gotgbot.InlineKeyboardButton{
		{cb("x", "answer|TOK|notanumber")},
	}); ok {
		t.Error("a non-numeric message id was accepted")
	}
}

func TestDonationRow(t *testing.T) {
	r := donationRow("https://example.com/donate")
	if len(r) != 1 {
		t.Fatalf("donationRow buttons = %d; want 1", len(r))
	}
	if r[0].Url != "https://example.com/donate" {
		t.Errorf("donation url = %q; want the donation link", r[0].Url)
	}
	if r[0].CallbackData != "" {
		t.Errorf("donation button should be a URL button, not a callback (data=%q)", r[0].CallbackData)
	}
}

func TestCancelMarkup(t *testing.T) {
	m := cancelMarkup()
	if len(m.InlineKeyboard) != 1 || len(m.InlineKeyboard[0]) != 1 {
		t.Fatalf("cancelMarkup shape = %v; want a single 1-button row", m.InlineKeyboard)
	}
	if m.InlineKeyboard[0][0].CallbackData != "cancel" {
		t.Errorf("cancel data = %q; want cancel", m.InlineKeyboard[0][0].CallbackData)
	}
}
