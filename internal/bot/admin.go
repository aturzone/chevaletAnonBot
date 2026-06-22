package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/db"
)

// errWrongSyntax is the Go stand-in for the Python (IndexError, ValueError,
// WrongSyntaxErr) trio that admin_cmd mapped to "wrong syntax".
var errWrongSyntax = errors.New("wrong syntax")

// adminCmd ports modules/admin.py admin_cmd. Non-admins get the generic
// "didn't understand" reply; admins get the /admin sub-command dispatcher.
func adminCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if !b.isAdmin(userid) {
		return b.otherMessagesTemplate(ctx)
	}

	text := strings.Fields(ctx.EffectiveMessage.Text)
	if len(text) > 0 {
		text = text[1:] // drop "/admin"
	}

	err := b.adminDispatch(tg, ctx, userid, text)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errWrongSyntax):
		return b.replyHTML(ctx, "wrong syntax. use <code>/admin help</code>", false)
	case db.IsUniqueViolation(err):
		return b.replyHTML(ctx, "wrong value (uid or whatever). use <code>/admin help</code>", false)
	default:
		// Python: reply f"error: {e.__class__} | {e}" (best effort).
		_ = b.replyText(ctx, fmt.Sprintf("error: %T | %v", err, err))
		return nil
	}
}

// at returns text[i] or errWrongSyntax (Python IndexError).
func at(text []string, i int) (string, error) {
	if i < 0 || i >= len(text) {
		return "", errWrongSyntax
	}
	return text[i], nil
}

func (b *Bot) adminDispatch(tg *gotgbot.Bot, ctx *ext.Context, userid string, text []string) error {
	arg1, err := at(text, 0)
	if err != nil {
		return err
	}
	msg := ctx.EffectiveMessage
	dbctx, cancel := b.bg()
	defer cancel()

	switch arg1 {
	case "help":
		txt, err := b.Texts.Get("admin")
		if err != nil {
			return err
		}
		return b.replyHTML(ctx, txt, false)

	case "send-mass-msg":
		if !(len(text) > 1 && text[1] == "YES") {
			return b.replyHTML(ctx, "send <code>/admin send-mass-msg YES</code> if you're sure", false)
		}
		if e := b.replyText(ctx, "queued for 7 seconds later..."); e != nil {
			return e
		}
		b.scheduleMassMsg(ctx)
		return nil

	case "send-msg":
		targetUID, err := at(text, 1)
		if err != nil {
			return err
		}
		if !(len(text) > 2 && text[2] == "YES") {
			return b.replyHTML(ctx, fmt.Sprintf("send <code>/admin send-msg %s YES</code> if you're sure", targetUID), false)
		}
		tid, perr := strconv.ParseInt(targetUID, 10, 64)
		if perr != nil {
			return errWrongSyntax
		}
		if msg.ReplyToMessage == nil {
			return b.replyText(ctx, "failed to send: no replied message")
		}
		if _, cerr := msg.ReplyToMessage.Copy(tg, tid, nil); cerr != nil {
			return b.replyText(ctx, fmt.Sprintf("failed to send: %v", cerr))
		}
		return b.replyHTML(ctx, "sent to "+b.getLinkUsername(targetUID), false)

	case "user-count":
		n, err := b.DB.UserCount(dbctx)
		if err != nil {
			return err
		}
		return b.replyText(ctx, fmt.Sprintf("%d users", n))

	case "stats":
		uid, err := at(text, 1)
		if err != nil {
			return err
		}
		banned, limit, serr := b.DB.UserStatus(dbctx, uid)
		if serr != nil {
			// A missing row (uid never started the bot) is the expected "not found"
			// case. ANY other error (connection drop, statement/ctx timeout) is a
			// genuine DB fault: return it so adminCmd's error switch surfaces the
			// actual error class to the admin ("error: <type> | <msg>", matching
			// Python admin_cmd's broad `except Exception`) instead of masking every
			// fault as "not started". Admin-only path; like Python it does not
			// itself page ERROR_CHAT_ID.
			if db.IsNoRows(serr) {
				return b.replyText(ctx, "user has not started the bot yet?")
			}
			return serr
		}
		return b.replyText(ctx, fmt.Sprintf("is_banned=%s\ncid_limit=%d", pyBool(banned), limit))

	case "ban", "unban":
		uid, err := at(text, 1)
		if err != nil {
			return err
		}
		if e := b.DB.BanAction(dbctx, uid, arg1 == "ban"); e != nil {
			return e
		}
		return b.replyText(ctx, "done.")

	case "cid":
		sub, err := at(text, 1)
		if err != nil {
			return err
		}
		switch sub {
		case "get":
			uid, err := at(text, 2)
			if err != nil {
				return err
			}
			limit, e := b.DB.GetCIDLimit(dbctx, uid)
			if e != nil {
				return e
			}
			return b.replyText(ctx, fmt.Sprintf("limit: %d", limit))
		case "set":
			uid, err := at(text, 2)
			if err != nil {
				return err
			}
			limStr, err := at(text, 3)
			if err != nil {
				return err
			}
			lim, perr := strconv.Atoi(limStr)
			if perr != nil {
				return errWrongSyntax
			}
			if e := b.DB.SetCIDLimit(dbctx, uid, lim); e != nil {
				return e
			}
		}
		return b.replyText(ctx, "done.")

	case "link":
		uid, err := at(text, 1)
		if err != nil {
			return err
		}
		return b.replyHTML(ctx, b.getLinkUsername(uid), false)

	case "report":
		return b.adminReport(ctx, dbctx, text)

	case "ai-url":
		if !b.Cfg.AIEnabled {
			return b.replyText(ctx, "AI is disabled (set AI_ENABLED=true to use it).")
		}
		sub, err := at(text, 1)
		if err != nil {
			return err
		}
		switch sub {
		case "get":
			return b.replyHTML(ctx, "Current AI URL:\n<code>"+b.Dyn.AIURL()+"</code>", false)
		case "set":
			v, err := at(text, 2)
			if err != nil {
				return err
			}
			b.Dyn.SetAIURL(v)
			return b.replyHTML(ctx, "AI URL updated to:\n<code>"+v+"</code>", false)
		case "reset":
			b.Dyn.ResetAIURL()
			return b.replyText(ctx, "AI URL reset to config default.")
		default:
			return errWrongSyntax
		}

	case "ai-session":
		if !b.Cfg.AIEnabled {
			return b.replyText(ctx, "AI is disabled (set AI_ENABLED=true to use it).")
		}
		sub, err := at(text, 1)
		if err != nil {
			return err
		}
		switch sub {
		case "get":
			return b.replyHTML(ctx, "Current AI Session ID:\n<code>"+b.Dyn.AISessionID()+"</code>", false)
		case "set":
			v, err := at(text, 2)
			if err != nil {
				return err
			}
			b.Dyn.SetAISessionID(v)
			return b.replyHTML(ctx, "AI Session ID updated to:\n<code>"+v+"</code>", false)
		case "reset":
			b.Dyn.ResetAISessionID()
			return b.replyText(ctx, "AI Session ID reset to config default.")
		default:
			return errWrongSyntax
		}

	case "backup":
		return b.adminBackup(tg, ctx)

	default:
		return errWrongSyntax
	}
}

// adminReport ports the /admin report add/del/get branch.
func (b *Bot) adminReport(ctx *ext.Context, dbctx context.Context, text []string) error {
	sub, err := at(text, 1)
	if err != nil {
		return err
	}
	reportID, err := at(text, 2)
	if err != nil {
		return err
	}
	switch sub {
	case "add":
		count, e := b.DB.AddReportID(dbctx, reportID)
		if e != nil {
			return e
		}
		return b.replyText(ctx, fmt.Sprintf("added %s\ncounter: %d", reportID, count))
	case "del":
		count, e := b.DB.DelReportID(dbctx, reportID)
		if e != nil {
			return e
		}
		if count == 0 {
			return b.replyText(ctx, "this report didn't exist")
		}
		return b.replyText(ctx, fmt.Sprintf("deleted %d instance(s) of report: %s", count, reportID))
	case "get":
		if reportID == "all" {
			reports, e := b.DB.GetAllReports(dbctx)
			if e != nil {
				return e
			}
			// Mirror the Python loop: flush a chunk once it grows past 3900 chars,
			// then send whatever remains.
			const sep = " | "
			var parts []string
			for key, val := range reports {
				if joined := strings.Join(parts, sep); len(joined) > 3900 {
					if e := b.replyText(ctx, joined); e != nil {
						return e
					}
					parts = nil // reset after flushing so chunks aren't re-sent
				}
				parts = append(parts, fmt.Sprintf("%s (%d)", key, val))
			}
			if len(parts) > 0 {
				return b.replyText(ctx, strings.Join(parts, sep))
			}
			return nil
		}
		count, e := b.DB.GetReportID(dbctx, reportID)
		if e != nil {
			return e
		}
		return b.replyText(ctx, strconv.Itoa(count))
	}
	return nil
}

// adminBackup ports the /admin backup branch: send the newest file in backups/.
func (b *Bot) adminBackup(tg *gotgbot.Bot, ctx *ext.Context) error {
	entries, err := os.ReadDir("backups")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return errWrongSyntax // Python's backups[-1] would IndexError -> wrong syntax
	}
	sort.Strings(names)
	latest := names[len(names)-1]
	f, err := os.Open(filepath.Join("backups", latest))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = ctx.EffectiveMessage.ReplyDocument(tg, gotgbot.InputFileByReader(latest, f),
		&gotgbot.SendDocumentOpts{Caption: "#backup"})
	return err
}

// pyBool renders a Go bool the way Python str(bool) does (True/False), matching
// the /admin stats output.
func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
