package bot

import (
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// This file ports modules/Global/reply_markups.py verbatim. The callback_data
// strings are reproduced exactly because the handlers match on them.

// MSG_BTN labels (the buttons under each delivered anonymous message).
const (
	msgBtnRepliedTo = "ریپلای شده به این پیام"
	msgBtnReply     = "⌨️ ارسال جواب"
	msgBtnSeen      = "✅ سین بزن"
	msgBtnSeenDone  = "☑️ سین زدم"
	msgBtnBlock     = "🔒 بلاک"
	msgBtnUnblock   = "🔓 آنبلاک"
	msgBtnReport    = "⚠️ ریپورت"
)

// cb builds a callback-data button.
func cb(text, data string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: text, CallbackData: data}
}

// urlBtn builds a URL button.
func urlBtn(text, url string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: text, Url: url}
}

// cancelButton mirrors reply_markups.CANCEL_BUTTON.
func cancelButton() gotgbot.InlineKeyboardButton { return cb("بیخیالش", "cancel") }

// cancelMarkup is the single-row [CANCEL_BUTTON] keyboard used by /start connect
// and the answer prompt.
func cancelMarkup() *gotgbot.InlineKeyboardMarkup {
	return &gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{cancelButton()}},
	}
}

// messageKeyboard builds the inline keyboard placed under every delivered
// anonymous message, mirroring the first two rows of reply_markup_keyboard in
// handler_templates.send_msg_template. senderEncChid is the already-encoded
// chevaletid of the sender; mid is the sender's message id. When seen is true
// the "mark as seen" button is inserted at the front of the first row, exactly
// as the Python `reply_markup_keyboard[0].insert(0, ...)`.
//
// The caller appends the optional "sent with link N (cid)" row and the donation
// row, matching the order the Python code assembles them.
func messageKeyboard(senderEncChid string, mid int64, seen bool) [][]gotgbot.InlineKeyboardButton {
	midStr := strconv.FormatInt(mid, 10)
	firstRow := []gotgbot.InlineKeyboardButton{
		cb(msgBtnReply, "answer|"+senderEncChid+"|"+midStr),
	}
	if seen {
		firstRow = append([]gotgbot.InlineKeyboardButton{
			cb(msgBtnSeen, "seen|"+senderEncChid+"|"+midStr),
		}, firstRow...)
	}
	return [][]gotgbot.InlineKeyboardButton{
		firstRow,
		{
			cb(msgBtnReport, "report|"+senderEncChid+"|"+midStr),
			cb(" ", "no-callback"),
			cb(msgBtnBlock, "block|"+senderEncChid),
		},
	}
}

// donationRow mirrors the trailing donation-link button row appended to every
// delivered message's keyboard.
func donationRow(donationLink string) []gotgbot.InlineKeyboardButton {
	return []gotgbot.InlineKeyboardButton{urlBtn(btnDonation, donationLink)}
}

// settingsMainMenu mirrors SETTINGS_MARKUP["main-menu-set"].
func settingsMainMenu() [][]gotgbot.InlineKeyboardButton {
	return [][]gotgbot.InlineKeyboardButton{
		{cb("⌨️ ارسال پیام بدون لینک | ریپلای به کانال", "easier-answer|")},
		{cb("✍️ ارسال به ادمین خاص کانال", "channel-signature|")},
		{cb("🔗 پیشنمایشِ لینک", "wpp|"), cb("👌 ارسال پیامهای پیوسته", "media-settings|")},
		{cb("🖋 ریپلای به بخشی از پیام", "reply-quote|"), cb("👀 نمایش دکمه سین زدن", "seen-settings|")},
		{cb("⚠️ اخطار پاک سازی پیام", "warning|"), cb("📛 تغییر نام نمایشی", "change-name|")},
		{cb("#️⃣ تگ آهنگ", "audio-tag|"), cb("#️⃣ تگ دلخواه", "custom-tag|")},
		{cb("🚫 آنبلاک شدن خودت", "unblock-me|"), cb("🚫 آنبلاک همه", "unblock-all|")},
	}
}

// settingsButtons mirrors the single-button entries of SETTINGS_MARKUP.
var settingsButtons = map[string]gotgbot.InlineKeyboardButton{
	"formatting":         cb("❔قالب بندی چیه", "what-is-formatting"),
	"back-to-menu":       cb("↪️ بازگشت به منوی اصلی", "settings-menu"),
	"nvm-back-to-menu":   cb("↪️ بیخیالش برگرد منوی اصلی", "settings-menu"),
	"wpp-activate":       cb("✅ برگشت به حالت پیشفرض", "wpp|activate"),
	"wpp-deactivate":     cb("❌ غیرفعال سازی اجباری", "wpp|deactivate"),
	"warning-activate":   cb("✅ فعالسازی", "warning|activate"),
	"warning-deactivate": cb("❌ غیرفعالسازی", "warning|deactivate"),
	"seen-activate":      cb("✅ فعالسازی", "seen-settings|activate"),
	"seen-deactivate":    cb("❌ غیرفعالسازی", "seen-settings|deactivate"),
	"remove-custom-tag":  cb("🗑 پاک کردن تگ دلخواه", "rm-custom-tag"),
	"remove-audio-tag":   cb("🗑 پاک کردن تگ آهنگ", "rm-audio-tag"),
}

// mylinksDefaultMenu mirrors MYLINKS_MARKUP["default-set"].
func mylinksDefaultMenu() [][]gotgbot.InlineKeyboardButton {
	return [][]gotgbot.InlineKeyboardButton{
		{cb("➕ اضافه کردن لینک جدید", "add-link")},
		{cb("🔧 شخصی سازی لینک", "ch-link"), cb("❌ حذف کردن لینک", "rm-link")},
		{cb("❔چرا چندتا لینک داشته باشم", "more-links")},
	}
}

// mylinksButtons mirrors the single-button entries of MYLINKS_MARKUP.
var mylinksButtons = map[string]gotgbot.InlineKeyboardButton{
	"back-to-menu":      cb("↪️ برگشت به منوی اصلی", "mylinks-menu"),
	"what-is-cid":       cb("❔آیدی لینک چیه", "what-is-cid|"),
	"what-is-customize": cb("❔شخصی سازی لینک چیه", "what-is-cid|"),
}
