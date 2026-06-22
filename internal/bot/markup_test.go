package bot

import "testing"

// TestMessageKeyboard locks the callback_data format of the buttons under every
// delivered anonymous message. These strings are a frozen compatibility contract
// (historical callback_data the handlers parse), so any drift is a parity break.
func TestMessageKeyboard(t *testing.T) {
	const chid = "ENCCHID"
	const mid int64 = 12345

	// without "seen": row0 = [answer], row1 = [report, spacer, block].
	kb := messageKeyboard(chid, mid, false)
	if len(kb) != 2 {
		t.Fatalf("rows = %d; want 2", len(kb))
	}
	if len(kb[0]) != 1 {
		t.Fatalf("row0 buttons = %d; want 1 (answer only)", len(kb[0]))
	}
	if kb[0][0].CallbackData != "answer|ENCCHID|12345" {
		t.Errorf("answer data = %q; want answer|ENCCHID|12345", kb[0][0].CallbackData)
	}
	if len(kb[1]) != 3 {
		t.Fatalf("row1 buttons = %d; want 3 (report, spacer, block)", len(kb[1]))
	}
	if kb[1][0].CallbackData != "report|ENCCHID|12345" {
		t.Errorf("report data = %q; want report|ENCCHID|12345", kb[1][0].CallbackData)
	}
	if kb[1][1].CallbackData != "no-callback" {
		t.Errorf("spacer data = %q; want no-callback", kb[1][1].CallbackData)
	}
	// block carries ONLY the chid (no mid) — blocking a sender, not a message.
	if kb[1][2].CallbackData != "block|ENCCHID" {
		t.Errorf("block data = %q; want block|ENCCHID", kb[1][2].CallbackData)
	}

	// with "seen": the seen button is inserted at the FRONT of row0.
	kbs := messageKeyboard(chid, mid, true)
	if len(kbs[0]) != 2 {
		t.Fatalf("row0 buttons (seen on) = %d; want 2", len(kbs[0]))
	}
	if kbs[0][0].CallbackData != "seen|ENCCHID|12345" {
		t.Errorf("seen data = %q; want seen|ENCCHID|12345 (must be first)", kbs[0][0].CallbackData)
	}
	if kbs[0][1].CallbackData != "answer|ENCCHID|12345" {
		t.Errorf("answer data (seen on) = %q; want answer|ENCCHID|12345 (after seen)", kbs[0][1].CallbackData)
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
