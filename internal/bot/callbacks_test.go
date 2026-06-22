package bot

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// TestTransformKeyboard guards the fix for the Python bug where rebuilding the
// keyboard dropped non-callback buttons: transformKeyboard must apply fn to every
// button, preserve the row shape, and return URL buttons (the donation link)
// verbatim so EditReplyMarkup doesn't silently fail.
func TestTransformKeyboard(t *testing.T) {
	kb := [][]gotgbot.InlineKeyboardButton{
		{cb(msgBtnSeen, "seen|CHID|5"), cb(msgBtnReply, "answer|CHID|5")},
		{cb(msgBtnReport, "report|CHID|5"), cb(" ", "no-callback"), cb(msgBtnBlock, "block|CHID")},
		{urlBtn("donate", "https://donate.example")},
	}

	// the block() swap fn: flip only the "block|" prefix to "unblock|".
	out := transformKeyboard(kb, func(btn gotgbot.InlineKeyboardButton) gotgbot.InlineKeyboardButton {
		if strings.HasPrefix(btn.CallbackData, "block") {
			return cb(msgBtnUnblock, "unblock|"+strings.TrimPrefix(btn.CallbackData, "block|"))
		}
		return btn
	})

	// row shape preserved.
	if len(out) != 3 || len(out[0]) != 2 || len(out[1]) != 3 || len(out[2]) != 1 {
		t.Fatalf("row shape = %v; want [2 3 1]", [][]gotgbot.InlineKeyboardButton{out[0], out[1], out[2]})
	}
	// the URL (donation) button survives verbatim — the crux of the fix.
	url := out[2][0]
	if url.Url != "https://donate.example" || url.CallbackData != "" {
		t.Errorf("URL button corrupted: url=%q data=%q", url.Url, url.CallbackData)
	}
	// the block button was swapped; the spacer/report/seen/answer are untouched.
	if out[1][2].CallbackData != "unblock|CHID" || out[1][2].Text != msgBtnUnblock {
		t.Errorf("block->unblock swap = (%q,%q)", out[1][2].Text, out[1][2].CallbackData)
	}
	if out[0][0].CallbackData != "seen|CHID|5" || out[1][1].CallbackData != "no-callback" {
		t.Error("non-target buttons must pass through verbatim")
	}
}

// TestBlockUnblockSwapPattern verifies the prefix-only swap never corrupts a
// chevaletid that itself contains the substring "block" (the reason the code
// swaps the prefix instead of Python's replace-all).
func TestBlockUnblockSwapPattern(t *testing.T) {
	swapBU := func(btn gotgbot.InlineKeyboardButton) gotgbot.InlineKeyboardButton {
		if strings.HasPrefix(btn.CallbackData, "block") {
			return cb(msgBtnUnblock, "unblock|"+strings.TrimPrefix(btn.CallbackData, "block|"))
		}
		return btn
	}
	in := [][]gotgbot.InlineKeyboardButton{{cb(msgBtnBlock, "block|aBLOCKb_block")}}
	out := transformKeyboard(in, swapBU)
	// only the leading token flips; the inner "block" in the token is preserved.
	if got := out[0][0].CallbackData; got != "unblock|aBLOCKb_block" {
		t.Errorf("swap = %q; want unblock|aBLOCKb_block (inner 'block' must survive)", got)
	}
}
