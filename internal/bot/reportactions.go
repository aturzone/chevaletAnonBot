package bot

import (
	"html"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters"
	msgfilters "github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

// Action buttons under every report posted to REPORT_CHAT_ID.
//
// Before this, an admin read the report, copied the uid out of the header, and
// ran /admin report add … or /admin ban … by hand. These four buttons do the same
// work in one tap, and add the ability to write to either party through the bot:
//
//	[ ✅ register report ] [ 🚫 ban ]
//	[ ✉️ message reporter ] [ ✉️ message reported ]
//
// Three things about the environment shape this code:
//
//  1. REPORT_CHAT_ID is a CHANNEL, and prep deliberately drops channel updates
//     (decorators.py parity), so the callback handler is registered outside prep —
//     the same exception errMore makes for the error chat.
//  2. In a channel the callback's From is the admin who tapped, which is what the
//     admin check and the "who did it" label use. It is checked against ADMINS
//     server-side; being able to see the channel is not authority to ban.
//  3. Composing a message cannot happen in the channel (that would show the draft
//     to everyone), so the bot asks in the admin's private chat instead.
const (
	btnRptAccept      = "✅ ثبت ریپورت"
	btnRptBan         = "🚫 بن کردن"
	btnRptMsgReporter = "✉️ پیام به ریپورت‌کننده"
	btnRptMsgReported = "✉️ پیام به ریپورت‌شده"
)

// Callback verbs. Kept to two characters because callback_data is capped at 64
// bytes and each carries a 31-char sealed token: "rpt|md|" + 31 = 38.
const (
	rptVerbAccept      = "a"
	rptVerbBan         = "b"
	rptVerbMsgReporter = "mr"
	rptVerbMsgReported = "md"
)

// composeMarker tags the prompt the bot sends to an admin's private chat. The
// admin's reply is matched by looking at the message it replies TO, so the pending
// compose survives a bot restart — there is no in-memory state to lose. The token
// after the marker is the sealed target uid, so a raw id never appears even here.
const composeMarker = "ref:rpt:"

// reportActionKeyboard is attached to the report header message.
//
// Sealed tokens rather than raw uids: the header already prints the reported uid
// for copy/paste, so this is not hiding anything from admins — it keeps the
// invariant that no real Telegram id is ever put in callback_data, which is what
// stops an id leaking if a button is ever surfaced somewhere less private.
func reportActionKeyboard(reporterToken, reportedToken string) gotgbot.InlineKeyboardMarkup {
	return ikb(
		row(
			cb(btnRptAccept, "rpt|"+rptVerbAccept+"|"+reportedToken),
			cb(btnRptBan, "rpt|"+rptVerbBan+"|"+reportedToken),
		),
		row(
			cb(btnRptMsgReporter, "rpt|"+rptVerbMsgReporter+"|"+reporterToken),
			cb(btnRptMsgReported, "rpt|"+rptVerbMsgReported+"|"+reportedToken),
		),
	)
}

// rptDoneRow replaces the accept/ban row once one of them has been used, so a
// second admin cannot double-ban or double-count the same report, and so the log
// of who handled it survives in the channel.
func rptDoneRow(label string) []gotgbot.InlineKeyboardButton {
	return row(cb(label, "no-callback"))
}

// adminLabel is a short human name for the admin who tapped, for the done label.
func adminLabel(u gotgbot.User) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return strconv.FormatInt(u.Id, 10)
}

// reportAction handles the four buttons. Registered OUTSIDE prep (channel chat).
func (b *Bot) reportAction(tg *gotgbot.Bot, ctx *ext.Context) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	msg := ctx.EffectiveMessage

	// Only ever act inside the configured report chat.
	if msg == nil || b.Cfg.ReportChatID == "" ||
		strconv.FormatInt(msg.Chat.Id, 10) != b.Cfg.ReportChatID {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}

	// Authority comes from ADMINS, not from channel membership.
	actor := strconv.FormatInt(clbk.From.Id, 10)
	if !b.isAdmin(actor) {
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "فقط ادمین‌ها می‌تونن از این دکمه‌ها استفاده کنن.",
			ShowAlert: true,
		})
		return nil
	}

	fields := strings.SplitN(clbk.Data, "|", 3)
	if len(fields) != 3 {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
	verb, token := fields[1], fields[2]

	uid, ok := b.Tokens.Open(token, nil)
	if !ok {
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "این دکمه دیگه معتبر نیست.",
			ShowAlert: true,
		})
		return nil
	}
	uidStr := strconv.FormatInt(uid, 10)

	switch verb {
	case rptVerbAccept:
		return b.rptAccept(tg, ctx, clbk, uidStr)
	case rptVerbBan:
		return b.rptBan(tg, ctx, clbk, uidStr)
	case rptVerbMsgReporter:
		return b.rptAskCompose(tg, clbk, token, "reporter")
	case rptVerbMsgReported:
		return b.rptAskCompose(tg, clbk, token, "reported")
	default:
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
}

// rptAccept records one report against the reported user, exactly as
// `/admin report add <uid>` did by hand, and reports the new total.
func (b *Bot) rptAccept(tg *gotgbot.Bot, ctx *ext.Context, clbk *gotgbot.CallbackQuery, uid string) error {
	dbctx, cancel := b.bg()
	defer cancel()

	count, err := b.DB.AddReportID(dbctx, uid)
	if err != nil {
		return err
	}
	_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
		Text:      "ثبت شد. تعداد ریپورت‌های این کاربر: " + strconv.Itoa(count),
		ShowAlert: true,
	})
	b.rptMarkDone(tg, ctx, "✅ ثبت شد ("+strconv.Itoa(count)+") — "+adminLabel(clbk.From))
	return nil
}

// rptBan bans the reported user and tells them, which is the one notification the
// bot sends here: they would discover it anyway the moment the bot stops
// answering, so saying it plainly is clearer than going silent. Accepting a report
// stays silent on purpose — a warning mostly invites a fresh account.
func (b *Bot) rptBan(tg *gotgbot.Bot, ctx *ext.Context, clbk *gotgbot.CallbackQuery, uid string) error {
	dbctx, cancel := b.bg()
	defer cancel()

	if err := b.DB.BanAction(dbctx, uid, true); err != nil {
		return err
	}
	_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
		Text:      "کاربر بن شد.",
		ShowAlert: true,
	})

	// Best effort: a user who never started the bot, or who blocked it, cannot be
	// told — that must not fail the ban, which is already committed.
	if uid64, perr := strconv.ParseInt(uid, 10, 64); perr == nil {
		_, _ = tg.SendMessage(uid64, txtBannedNotice, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	}
	b.rptMarkDone(tg, ctx, "🚫 بن شد — "+adminLabel(clbk.From))
	return nil
}

// rptMarkDone swaps the accept/ban row for a dead status button, leaving the two
// message buttons usable (writing to either party still makes sense afterwards).
func (b *Bot) rptMarkDone(tg *gotgbot.Bot, ctx *ext.Context, label string) {
	msg := ctx.EffectiveMessage
	if msg == nil || msg.ReplyMarkup == nil {
		return
	}
	rows := msg.ReplyMarkup.InlineKeyboard
	newRows := make([][]gotgbot.InlineKeyboardButton, 0, len(rows))
	for _, r := range rows {
		if rowHasVerb(r, rptVerbAccept) || rowHasVerb(r, rptVerbBan) {
			newRows = append(newRows, rptDoneRow(label))
			continue
		}
		newRows = append(newRows, r)
	}
	// Errors ignored deliberately: the action already succeeded, and Telegram
	// rejects an unchanged markup — see tgerr.go's "message is not modified".
	_, _, _ = msg.EditReplyMarkup(tg, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: newRows},
	})
}

// rowHasVerb reports whether a keyboard row carries one of the rpt verbs.
func rowHasVerb(r []gotgbot.InlineKeyboardButton, verb string) bool {
	for _, btn := range r {
		if strings.HasPrefix(btn.CallbackData, "rpt|"+verb+"|") {
			return true
		}
	}
	return false
}

// rptAskCompose asks the admin, in their own private chat, for the text to send.
//
// It has to be private: a draft typed into the report channel would be visible to
// everyone there. The prompt carries the sealed target uid, and the admin's reply
// is matched against the prompt, so nothing needs to be remembered in memory and
// a restart mid-compose is harmless.
func (b *Bot) rptAskCompose(tg *gotgbot.Bot, clbk *gotgbot.CallbackQuery, token, role string) error {
	who := "ریپورت‌کننده"
	if role == "reported" {
		who = "کاربر ریپورت‌شده"
	}

	prompt := "✉️ <b>ارسال پیام به " + who + "</b>\n\n" +
		"متن پیام رو در جواب همین پیام بنویس. پیام با اسم بات براش فرستاده میشه.\n" +
		"برای انصراف /cancel رو بفرست.\n\n" +
		"<code>" + composeMarker + role + ":" + token + "</code>"

	_, err := tg.SendMessage(clbk.From.Id, prompt, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: gotgbot.ForceReply{ForceReply: true, InputFieldPlaceholder: "متن پیام…"},
	})
	if err != nil {
		// Almost always "bot can't initiate conversation with a user": the admin
		// has never opened a private chat with the bot.
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "نشد برات پیام خصوصی بفرستم. اول یه /start به بات بده بعد دوباره امتحان کن.",
			ShowAlert: true,
		})
		return nil
	}
	_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
		Text:      "توی چت خصوصی بات، متن پیام رو بنویس.",
		ShowAlert: true,
	})
	return nil
}

// rptComposeFilter matches an admin's reply to a compose prompt: a private text
// message replying to a bot message that carries the marker. Narrow on purpose —
// it is registered ahead of the conversations, so it must not match anything else.
func (b *Bot) rptComposeFilter(botID int64) filters.Message {
	return func(m *gotgbot.Message) bool {
		return m.Chat.Type == "private" &&
			msgfilters.Text(m) &&
			m.ReplyToMessage != nil &&
			m.ReplyToMessage.From != nil &&
			m.ReplyToMessage.From.Id == botID &&
			strings.Contains(m.ReplyToMessage.Text, composeMarker)
	}
}

// rptComposeReply delivers what the admin wrote to the party the prompt names.
func rptComposeReply(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	if msg == nil || msg.ReplyToMessage == nil {
		return nil
	}
	// Re-check authority: the prompt is old and the admin list may have changed.
	if !b.isAdmin(userid) {
		return nil
	}
	if strings.TrimSpace(msg.Text) == "/cancel" {
		return b.replyText(ctx, "لغو شد.")
	}

	role, token, ok := parseComposeRef(msg.ReplyToMessage.Text)
	if !ok {
		return nil
	}
	uid, ok := b.Tokens.Open(token, nil)
	if !ok {
		return b.replyText(ctx, "این درخواست دیگه معتبر نیست.")
	}

	body := "📩 <b>پیام از طرف پشتیبانی</b>\n\n" + html.EscapeString(msg.Text)
	if _, err := tg.SendMessage(uid, body, &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
		// A user who blocked the bot or never started it is an expected outcome
		// here, not a bug worth an error report.
		return b.replyText(ctx, "نشد پیام رو بفرستم — احتمالا کاربر بات رو بلاک کرده یا هیچوقت استارت نزده.")
	}

	who := "ریپورت‌کننده"
	if role == "reported" {
		who = "کاربر ریپورت‌شده"
	}
	return b.replyText(ctx, "✅ پیام برای "+who+" فرستاده شد.")
}

// parseComposeRef pulls the role and sealed token back out of a prompt.
//
// The token is cut at the first character that cannot appear in base64url rather
// than at whitespace. Telegram strips HTML from Message.Text (the tags arrive as
// entities), so in practice the marker ends the text cleanly — but if that ever
// stopped holding, a trailing "</code>" would ride along inside the token, Open
// would fail, and the compose flow would break silently. Cutting on the charset
// makes the parser independent of that assumption.
func parseComposeRef(promptText string) (role, token string, ok bool) {
	i := strings.Index(promptText, composeMarker)
	if i < 0 {
		return "", "", false
	}
	rest := promptText[i+len(composeMarker):]

	role, rest, found := strings.Cut(rest, ":")
	if !found || (role != "reporter" && role != "reported") {
		return "", "", false
	}
	end := len(rest)
	for j := 0; j < len(rest); j++ {
		if !isBase64URLChar(rest[j]) {
			end = j
			break
		}
	}
	token = rest[:end]
	if token == "" {
		return "", "", false
	}
	return role, token, true
}

// isBase64URLChar reports whether c can appear in a RawURLEncoding token.
func isBase64URLChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_'
}
