package bot

import (
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/db"
)

// /admin_reports — the moderation panel, in the admin's private chat.
//
// Before this, reviewing reports meant `/admin report get all` (a wall of uids)
// followed by hand-typed `/admin ban` and `/admin unban`, with the uid copied
// between them. This lists reported users with their names, report counts and ban
// state as buttons, and each user's panel unbans in one tap.
//
// Private chat only, on purpose: these are callback buttons, and callback buttons
// on a channel post never reach the bot (see reportactions.go).
const (
	modPageSize = 8 // users per page; keeps the keyboard tappable on a phone

	// Callback verbs. "am" = admin moderation.
	modVerbPageReported = "pr" // list reported, page N
	modVerbPageBanned   = "pb" // list banned, page N
	modVerbView         = "v"  // open one user's panel
	modVerbUnban        = "u"
	modVerbBan          = "b"
	modVerbClear        = "c" // clear that user's reports
)

// adminReportsCmd opens the panel.
func adminReportsCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if !b.isAdmin(userid) {
		return b.otherMessagesTemplate(ctx)
	}
	text, kb, err := b.modList(false, 0)
	if err != nil {
		return err
	}
	_, err = ctx.EffectiveMessage.Reply(tg, text, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: kb,
	})
	return err
}

// modList renders one page of either the reported or the banned list.
func (b *Bot) modList(banned bool, page int) (string, gotgbot.InlineKeyboardMarkup, error) {
	dbctx, cancel := b.bg()
	defer cancel()

	var (
		users []db.ModUser
		err   error
		title string
		verb  string
	)
	if banned {
		users, err = b.DB.GetBannedUsers(dbctx)
		title, verb = "🚫 <b>کاربران بن شده</b>", modVerbPageBanned
	} else {
		users, err = b.DB.GetReportedUsers(dbctx)
		title, verb = "⚠️ <b>کاربران ریپورت شده</b>", modVerbPageReported
	}
	if err != nil {
		return "", gotgbot.InlineKeyboardMarkup{}, err
	}

	if len(users) == 0 {
		empty := "هیچ کاربر ریپورت شده‌ای نیست 👌"
		if banned {
			empty = "هیچ کاربر بن شده‌ای نیست 👌"
		}
		return title + "\n\n" + empty,
			ikb(row(b.modSwitchButton(banned)), row(modBackToPanel())), nil
	}

	// Clamp rather than error: a page can go stale when another admin acts.
	pages := (len(users) + modPageSize - 1) / modPageSize
	if page >= pages {
		page = pages - 1
	}
	if page < 0 {
		page = 0
	}
	start := page * modPageSize
	end := min(start+modPageSize, len(users))

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteString(fmt.Sprintf("\nصفحه %d از %d — مجموع %d کاربر\n", page+1, pages, len(users)))
	sb.WriteString("\nروی هر کاربر بزن تا گزینه‌هاش باز شه.")

	var rows [][]gotgbot.InlineKeyboardButton
	for _, u := range users[start:end] {
		tok, err := b.Tokens.Seal(mustInt64(u.UID), nil)
		if err != nil {
			return "", gotgbot.InlineKeyboardMarkup{}, err
		}
		rows = append(rows, row(cb(modUserLabel(u), "am|"+modVerbView+"|"+tok)))
	}

	// Paging row, only when there is more than one page.
	if pages > 1 {
		var nav []gotgbot.InlineKeyboardButton
		if page > 0 {
			nav = append(nav, cb("◀️ قبلی", "am|"+verb+"|"+strconv.Itoa(page-1)))
		}
		if page+1 < pages {
			nav = append(nav, cb("بعدی ▶️", "am|"+verb+"|"+strconv.Itoa(page+1)))
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
	}
	rows = append(rows, row(b.modSwitchButton(banned)))
	// Always here, not appended by the caller: drilling into a user and coming back
	// to the list used to lose the route to the admin panel, leaving the list as a
	// dead end.
	rows = append(rows, row(modBackToPanel()))

	return sb.String(), gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}, nil
}

// modBackToPanel is the shared route out of the moderation screens.
func modBackToPanel() gotgbot.InlineKeyboardButton {
	return cb("🛡 پنل ادمین", "menu|"+menuAdmin)
}

// modSwitchButton toggles between the reported and banned lists.
func (b *Bot) modSwitchButton(showingBanned bool) gotgbot.InlineKeyboardButton {
	if showingBanned {
		return cb("⚠️ لیست ریپورت شده‌ها", "am|"+modVerbPageReported+"|0")
	}
	return cb("🚫 لیست بن شده‌ها", "am|"+modVerbPageBanned+"|0")
}

// modUserLabel is one button's caption: name, report count, and a ban marker.
func modUserLabel(u db.ModUser) string {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = u.UID
	}
	// Telegram truncates long button captions, so cap the name and keep the
	// counters — those are the part an admin is scanning for.
	if len(name) > 24 {
		name = name[:24] + "…"
	}
	label := fmt.Sprintf("%s — %d ریپورت", name, u.Reports)
	if u.Banned {
		label = "🚫 " + label
	}
	return label
}

// modUserPanel renders one user's actions.
func (b *Bot) modUserPanel(uid, token string) (string, gotgbot.InlineKeyboardMarkup, error) {
	dbctx, cancel := b.bg()
	defer cancel()

	u, err := b.DB.GetModUser(dbctx, uid)
	if err != nil {
		return "", gotgbot.InlineKeyboardMarkup{}, err
	}

	state := "✅ فعال"
	if u.Banned {
		state = "🚫 بن شده"
	}
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = "(بدون نام)"
	}
	text := "👤 <b>" + html.EscapeString(name) + "</b>\n" +
		"آیدی: " + hrefUser(u.UID, "") + "\n" +
		"وضعیت: " + state + "\n" +
		"ریپورت‌ها: <b>" + strconv.Itoa(u.Reports) + "</b>"

	var actions []gotgbot.InlineKeyboardButton
	if u.Banned {
		actions = append(actions, cb("🔓 آنبلاک (رفع بن)", "am|"+modVerbUnban+"|"+token))
	} else {
		actions = append(actions, cb("🚫 بن کردن", "am|"+modVerbBan+"|"+token))
	}
	rows := [][]gotgbot.InlineKeyboardButton{actions}
	if u.Reports > 0 {
		rows = append(rows, row(cb("🧹 پاک کردن ریپورت‌ها", "am|"+modVerbClear+"|"+token)))
	}
	rows = append(rows,
		row(cb("✉️ پیام به این کاربر", "rpt|"+rptVerbMsgReported+"|"+token)),
		row(cb("↩️ برگشت به لیست", "am|"+modVerbPageReported+"|0")),
	)
	return text, gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}, nil
}

// adminMod handles every "am|" button. Registered outside prep and admin-checked
// here, matching the other admin callback paths.
func (b *Bot) adminMod(tg *gotgbot.Bot, ctx *ext.Context) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	actor := strconv.FormatInt(clbk.From.Id, 10)
	if !b.isAdmin(actor) {
		slog.Warn("admin moderation button used by a non-admin", "actor", actor)
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "فقط ادمین‌ها.",
			ShowAlert: true,
		})
		return nil
	}

	fields := strings.SplitN(clbk.Data, "|", 3)
	if len(fields) != 3 {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
	verb, arg := fields[1], fields[2]

	switch verb {
	case modVerbPageReported, modVerbPageBanned:
		page, _ := strconv.Atoi(arg)
		text, kb, err := b.modList(verb == modVerbPageBanned, page)
		if err != nil {
			return err
		}
		_, _ = clbk.Answer(tg, nil)
		return b.modEdit(tg, ctx, text, kb)

	case modVerbView, modVerbUnban, modVerbBan, modVerbClear:
		uid, ok := b.Tokens.Open(arg, nil)
		if !ok {
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "این دکمه دیگه معتبر نیست.",
				ShowAlert: true,
			})
			return nil
		}
		uidStr := strconv.FormatInt(uid, 10)

		dbctx, cancel := b.bg()
		defer cancel()
		switch verb {
		case modVerbUnban:
			if err := b.DB.BanAction(dbctx, uidStr, false); err != nil {
				return err
			}
			slog.Info("user unbanned by admin", "target", uidStr, "actor", actor)
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: "بن برداشته شد ✅", ShowAlert: true})
		case modVerbBan:
			if err := b.DB.BanAction(dbctx, uidStr, true); err != nil {
				return err
			}
			slog.Info("user banned by admin", "target", uidStr, "actor", actor)
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: "کاربر بن شد 🚫", ShowAlert: true})
		case modVerbClear:
			n, err := b.DB.DelReportID(dbctx, uidStr)
			if err != nil {
				return err
			}
			slog.Info("reports cleared by admin", "target", uidStr, "count", n, "actor", actor)
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
				Text:      strconv.Itoa(n) + " ریپورت پاک شد 🧹",
				ShowAlert: true,
			})
		default:
			_, _ = clbk.Answer(tg, nil)
		}

		// Always re-render the panel afterwards, so the state an admin sees is the
		// state in the database rather than what it was before their tap.
		text, kb, err := b.modUserPanel(uidStr, arg)
		if err != nil {
			return err
		}
		return b.modEdit(tg, ctx, text, kb)

	default:
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
}

// modEdit replaces the panel in place. "message is not modified" is expected when
// a tap produces no visible change and is swallowed by tgerr's benign-error list.
func (b *Bot) modEdit(tg *gotgbot.Bot, ctx *ext.Context, text string, kb gotgbot.InlineKeyboardMarkup) error {
	msg := ctx.EffectiveMessage
	if msg == nil {
		return nil
	}
	_, _, err := msg.EditText(tg, text, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: kb,
	})
	return err
}
