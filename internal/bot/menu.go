package bot

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// /menu — a BotFather-style panel: ONE message that is edited in place as the user
// navigates, with a back button, rather than a trail of new messages.
//
// Four sections for everyone, a fifth for admins:
//
//	[ 🔗 my links ]  [ ⚙️ settings ]
//	[ 🆘 help     ]  [ 🙏 support  ]
//	[ 🛡 admin panel ]            (admins only)
//
// TWO DESIGN POINTS THAT MATTER:
//
// 1. "my links" and "settings" are ConversationHandlers with their own state
// machines. Rather than reimplement them, their buttons carry the callback data
// those conversations ALREADY accept as entry points (mylinks-menu / settings-menu
// — see myLinksConversation and settingsConversation). So tapping them enters the
// real conversation, and every existing sub-flow keeps working untouched. Anything
// else would have meant duplicating two state machines, which is how menus and the
// commands they wrap drift apart.
//
// 2. Every other menu callback is prefixed "menu|", which is checked against the
// conversations' cqContains filters so it cannot be swallowed by one. The handler
// is also registered BEFORE the conversations, so a user part-way through a
// settings flow can still tap the menu.

// menuVerb values. Kept free of the substrings the conversation entry points match
// on (settings-menu, mylinks-menu, add-link, what-is-cid, more-links, …).
const (
	menuMain     = "main"
	menuHelp     = "help"
	menuHelpText = "helptext"
	menuPrivacy  = "privacy"
	menuMyUID    = "myuid"
	menuBug      = "bug"
	menuDonate   = "donate"

	// Admin section.
	menuAdmin        = "admin"
	menuAdminStats   = "astats"
	menuAdminReports = "areports"
	menuAdminDonate  = "adonate"
	menuAdminDonOn   = "adon_on"
	menuAdminDonOff  = "adon_off"
	menuAdminBackup  = "abackup"
	menuAdminCount   = "acount"
	menuAdminCmds    = "acmds"
)

// menuTitle is the panel's own heading, shown on the main screen.
const menuTitle = "🏠 <b>منوی اصلی</b>\n\nیکی از گزینه‌ها رو انتخاب کن:"

// panelTextLimit is where editing stops being safe. Telegram's hard cap is 4096;
// privacy_safety.txt alone is ~3900, so a panel that grows a header would silently
// fail to render. Past this, send a fresh message instead of editing.
const panelTextLimit = 4000

// mainMenuKeyboard builds the top level. The admin row only exists for admins —
// a non-admin must not see that there is a fifth section.
func mainMenuKeyboard(isAdmin bool) gotgbot.InlineKeyboardMarkup {
	rows := [][]gotgbot.InlineKeyboardButton{
		{
			// Straight into the existing conversations (see the note above).
			cb("🔗 لینک‌های من", "mylinks-menu"),
			cb("⚙️ تنظیمات و قابلیت‌ها", "settings-menu"),
		},
		{
			cb("🆘 راهنما", "menu|"+menuHelp),
			cb("🙏 حمایت مالی", "menu|"+menuDonate),
		},
	}
	if isAdmin {
		rows = append(rows, row(cb("🛡 پنل ادمین", "menu|"+menuAdmin)))
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// backRow is the navigation every sub-screen carries.
func backRow() []gotgbot.InlineKeyboardButton {
	return row(cb("↩️ برگشت به منو", "menu|"+menuMain))
}

// menuCmd handles /menu.
func menuCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	_, err := ctx.EffectiveMessage.Reply(tg, menuTitle, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuKeyboard(b.isAdmin(userid)),
	})
	return err
}

// menuCallback handles every "menu|" button. Registered outside prep, before the
// conversations, and it resolves the acting user itself.
func (b *Bot) menuCallback(tg *gotgbot.Bot, ctx *ext.Context) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	userid := strconv.FormatInt(clbk.From.Id, 10)
	verb := strings.TrimPrefix(clbk.Data, "menu|")
	admin := b.isAdmin(userid)

	// Every admin screen is gated here, not just hidden from the keyboard: the
	// button data is guessable, and hiding a button is not a permission check.
	//
	// Gated on isAdminVerb alone. An earlier version also required the verb to start
	// with "a", which happens to be true of all of them today — and would have
	// silently skipped the check for any admin verb added later that did not.
	if isAdminVerb(verb) && !admin {
		slog.Warn("menu admin screen requested by a non-admin", "actor", userid, "verb", verb)
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "فقط ادمین‌ها.",
			ShowAlert: true,
		})
		return nil
	}

	switch verb {
	case menuMain:
		_, _ = clbk.Answer(tg, nil)
		return b.panelEdit(tg, ctx, menuTitle, mainMenuKeyboard(admin))

	case menuHelp:
		_, _ = clbk.Answer(tg, nil)
		return b.panelEdit(tg, ctx, "🆘 <b>راهنما</b>\n\nچی میخوای بدونی؟", ikb(
			row(cb("📖 راهنمای کامل", "menu|"+menuHelpText)),
			row(cb("🔒 حریم خصوصی", "menu|"+menuPrivacy)),
			row(cb("🆔 آیدی عددی من", "menu|"+menuMyUID), cb("🐞 گزارش باگ", "menu|"+menuBug)),
			backRow(),
		))

	case menuHelpText:
		_, _ = clbk.Answer(tg, nil)
		txt, err := b.Texts.Get("start_help")
		if err != nil {
			return err
		}
		txt = strings.ReplaceAll(txt, "%s", b.Dyn.DonationLink())
		return b.panelEdit(tg, ctx, txt, ikb(backRow()))

	case menuPrivacy:
		_, _ = clbk.Answer(tg, nil)
		txt, err := b.Texts.Get("privacy_safety")
		if err != nil {
			return err
		}
		// Deliberately no added heading: this text is ~3900 chars and a header could
		// push the edit past Telegram's 4096 limit.
		return b.panelEdit(tg, ctx, txt, ikb(backRow()))

	case menuDonate:
		_, _ = clbk.Answer(tg, nil)
		txt, err := b.Texts.Get("donate")
		if err != nil {
			return err
		}
		txt = strings.ReplaceAll(txt, "%s", b.Dyn.DonationLink())
		return b.panelEdit(tg, ctx, txt, ikb(backRow()))

	case menuMyUID:
		_, _ = clbk.Answer(tg, nil)
		return b.panelEdit(tg, ctx,
			"🆔 آیدی عددی تو:\n<code>"+userid+"</code>", ikb(backRow()))

	case menuBug:
		_, _ = clbk.Answer(tg, nil)
		// Same text /bug serves (warn_reply_to_channel), so the two can never drift.
		txt, err := b.Texts.Get("warn_reply_to_channel")
		if err != nil {
			return err
		}
		return b.panelEdit(tg, ctx, txt, ikb(backRow()))

	// ---------------------------------------------------------------- admin
	case menuAdmin:
		_, _ = clbk.Answer(tg, nil)
		return b.panelEdit(tg, ctx, adminPanelTitle, adminPanelKeyboard())

	case menuAdminStats:
		_, _ = clbk.Answer(tg, nil)
		dbctx, cancel := b.bg()
		defer cancel()
		s, err := b.DB.GetStats(dbctx, 7)
		if err != nil {
			return err
		}
		return b.panelEdit(tg, ctx, formatStats(s), ikb(
			row(cb("🔄 بروزرسانی", "menu|"+menuAdminStats)),
			row(cb("↩️ پنل ادمین", "menu|"+menuAdmin)),
		))

	case menuAdminReports:
		_, _ = clbk.Answer(tg, nil)
		text, kb, err := b.modList(false, 0)
		if err != nil {
			return err
		}
		// modList's own keyboard already navigates; add a way back to the panel.
		kb.InlineKeyboard = append(kb.InlineKeyboard, row(cb("↩️ پنل ادمین", "menu|"+menuAdmin)))
		return b.panelEdit(tg, ctx, text, kb)

	case menuAdminDonate:
		_, _ = clbk.Answer(tg, nil)
		return b.panelEdit(tg, ctx, b.donateStatus(), b.adminDonateKeyboard())

	case menuAdminDonOn, menuAdminDonOff:
		b.Dyn.SetDonationEnabled(verb == menuAdminDonOn)
		slog.Info("donation button toggled from the menu", "on", verb == menuAdminDonOn, "actor", userid)
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: "ذخیره شد ✅"})
		return b.panelEdit(tg, ctx, b.donateStatus(), b.adminDonateKeyboard())

	case menuAdminCount:
		_, _ = clbk.Answer(tg, nil)
		dbctx, cancel := b.bg()
		defer cancel()
		n, err := b.DB.UserCount(dbctx)
		if err != nil {
			return err
		}
		return b.panelEdit(tg, ctx, fmt.Sprintf("👥 تعداد کل کاربران: <b>%d</b>", n), ikb(
			row(cb("↩️ پنل ادمین", "menu|"+menuAdmin)),
		))

	case menuAdminBackup:
		// Sends a document, so it cannot be an in-place edit; the panel stays put.
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: "دارم بکاپ رو می‌فرستم…"})
		if err := b.adminBackup(tg, ctx); err != nil {
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "بکاپی پیدا نشد.",
				ShowAlert: true,
			})
			slog.Warn("menu backup failed", "err", err)
		}
		return nil

	case menuAdminCmds:
		_, _ = clbk.Answer(tg, nil)
		txt, err := b.Texts.Get("admin")
		if err != nil {
			return err
		}
		return b.panelEdit(tg, ctx, txt, ikb(row(cb("↩️ پنل ادمین", "menu|"+menuAdmin))))

	default:
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
}

const adminPanelTitle = "🛡 <b>پنل ادمین</b>\n\nگزینه‌های مدیریتی:"

// adminPanelKeyboard is the fifth section. Everything that works as a button is a
// button; the commands that need typed arguments (ban by uid, send-msg, cid/link
// tools, AI settings) are one tap away under "دستورات ادمین" rather than being
// pretended into buttons they cannot be.
func adminPanelKeyboard() gotgbot.InlineKeyboardMarkup {
	return ikb(
		row(cb("📊 آمار روزانه", "menu|"+menuAdminStats)),
		row(cb("⚠️ ریپورت‌ها و بن‌ها", "menu|"+menuAdminReports)),
		row(cb("❤️ دکمه دونیشن", "menu|"+menuAdminDonate), cb("👥 تعداد کاربران", "menu|"+menuAdminCount)),
		row(cb("💾 گرفتن بکاپ", "menu|"+menuAdminBackup)),
		row(cb("📋 همه دستورات ادمین", "menu|"+menuAdminCmds)),
		backRow(),
	)
}

// adminDonateKeyboard offers the toggle that matches the current state, so the
// visible button is always the action rather than the status.
func (b *Bot) adminDonateKeyboard() gotgbot.InlineKeyboardMarkup {
	toggle := cb("✅ فعال کردن", "menu|"+menuAdminDonOn)
	if b.Dyn.DonationEnabled() {
		toggle = cb("❌ غیرفعال کردن", "menu|"+menuAdminDonOff)
	}
	return ikb(
		row(toggle),
		row(cb("↩️ پنل ادمین", "menu|"+menuAdmin)),
	)
}

// isAdminVerb reports whether a verb belongs to the admin section. Explicit list
// rather than a prefix test, so a future user-facing verb starting with "a" cannot
// accidentally become admin-gated (or worse, an admin one slip through).
func isAdminVerb(verb string) bool {
	switch verb {
	case menuAdmin, menuAdminStats, menuAdminReports, menuAdminDonate,
		menuAdminDonOn, menuAdminDonOff, menuAdminBackup, menuAdminCount, menuAdminCmds:
		return true
	}
	return false
}

// panelEdit replaces the panel in place, which is what makes this feel like one
// screen rather than a growing chat.
//
// Falls back to a NEW message when the text is too long to edit safely, or when the
// edit fails (the panel may be too old to edit, or the callback may have arrived
// from a message the bot cannot modify). Losing the in-place feel is better than
// showing the user nothing.
func (b *Bot) panelEdit(tg *gotgbot.Bot, ctx *ext.Context, text string, kb gotgbot.InlineKeyboardMarkup) error {
	msg := ctx.EffectiveMessage
	if msg == nil || len([]rune(text)) > panelTextLimit {
		return b.panelSend(tg, ctx, text, kb)
	}
	if _, _, err := msg.EditText(tg, text, &gotgbot.EditMessageTextOpts{
		ParseMode:          "HTML",
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		ReplyMarkup:        kb,
	}); err != nil {
		if errMessageNotModified(err) {
			return nil // the user re-tapped the screen they are already on
		}
		slog.Warn("menu panel edit failed; sending a new panel", "err", err)
		return b.panelSend(tg, ctx, text, kb)
	}
	return nil
}

// panelSend posts a fresh panel.
func (b *Bot) panelSend(tg *gotgbot.Bot, ctx *ext.Context, text string, kb gotgbot.InlineKeyboardMarkup) error {
	chatID := int64(0)
	if ctx.EffectiveChat != nil {
		chatID = ctx.EffectiveChat.Id
	} else if ctx.CallbackQuery != nil {
		chatID = ctx.CallbackQuery.From.Id
	}
	if chatID == 0 {
		return nil
	}
	_, err := tg.SendMessage(chatID, text, &gotgbot.SendMessageOpts{
		ParseMode:          "HTML",
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		ReplyMarkup:        kb,
	})
	return err
}
