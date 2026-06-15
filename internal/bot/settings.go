package bot

import (
	"context"
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// This file ports modules/settings.py: the /settings ConversationHandler (states
// 0 = name, 1 = custom tag, 2 = audio tag) plus the wpp/seen/warning toggles,
// the static feature-explanation pages, and unblock-me / unblock-all.

// settingsCmd ports settings_cmd_clbk: shows the main settings menu, either by
// editing the current message (callback) or replying (the /settings command).
func settingsCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	txt, err := b.Texts.Get("settings/main")
	if err != nil {
		return err
	}
	markup := ikb(settingsMainMenu()...)
	if ctx.CallbackQuery != nil {
		_, _ = ctx.CallbackQuery.Answer(tg, nil)
		if _, _, e := ctx.EffectiveMessage.EditText(tg, txt, &gotgbot.EditMessageTextOpts{
			ParseMode: "HTML", ReplyMarkup: markup,
		}); e != nil {
			return e
		}
	} else {
		if _, e := ctx.EffectiveMessage.Reply(tg, txt, &gotgbot.SendMessageOpts{
			ParseMode: "HTML", ReplyMarkup: markup,
		}); e != nil {
			return e
		}
	}
	return handlers.EndConversation()
}

// editPage answers the callback and edits the message to a static settings page
// with the given keyboard. Used by the explanation pages.
func (b *Bot) editPage(tg *gotgbot.Bot, ctx *ext.Context, textKey string, markup gotgbot.InlineKeyboardMarkup) error {
	if ctx.CallbackQuery == nil || ctx.CallbackQuery.Data == "" {
		return nil
	}
	_, _ = ctx.CallbackQuery.Answer(tg, nil)
	txt, err := b.Texts.Get(textKey)
	if err != nil {
		return err
	}
	if _, _, e := ctx.EffectiveMessage.EditText(tg, txt, &gotgbot.EditMessageTextOpts{
		ParseMode: "HTML", ReplyMarkup: markup,
	}); e != nil {
		return e
	}
	return handlers.EndConversation()
}

func mediaSettings(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	return b.editPage(tg, ctx, "settings/media_settings",
		ikb(row(settingsButtons["formatting"], settingsButtons["back-to-menu"])))
}

func replyQuote(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	return b.editPage(tg, ctx, "settings/reply_quote", ikb(row(settingsButtons["back-to-menu"])))
}

func easierAnswer(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	return b.editPage(tg, ctx, "settings/easier_answer", ikb(row(settingsButtons["back-to-menu"])))
}

func channelSignature(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	return b.editPage(tg, ctx, "settings/channel_signature", ikb(row(settingsButtons["back-to-menu"])))
}

// toggleSpec describes one of the on/off settings (wpp, warning, seen).
type toggleSpec struct {
	textKey       string
	get           func(context.Context, string) (bool, error)
	set           func(context.Context, string, bool) error
	trueWord      string
	falseWord     string
	activateBtn   gotgbot.InlineKeyboardButton
	deactivateBtn gotgbot.InlineKeyboardButton
	ansActivate   string
	ansDeactivate string
}

// settingsToggle ports the shared shape of wpp_clbk / warning_clbk /
// seen_settings_clbk: "<key>|" renders the current state, "<key>|activate" /
// "<key>|deactivate" flips it (with a popup) then renders.
func (b *Bot) settingsToggle(tg *gotgbot.Bot, ctx *ext.Context, userid string, spec toggleSpec) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)

	dbctx, cancel := b.bg()
	defer cancel()

	render := func() error {
		cur, err := spec.get(dbctx, userid)
		if err != nil {
			return err
		}
		word := spec.trueWord
		btn := spec.deactivateBtn
		if !cur {
			word = spec.falseWord
			btn = spec.activateBtn
		}
		txt, err := b.Texts.Get(spec.textKey)
		if err != nil {
			return err
		}
		_, _, e := ctx.EffectiveMessage.EditText(tg, fmtText(txt, word), &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: ikb(row(btn), row(settingsButtons["back-to-menu"])),
		})
		return e
	}

	// data is "<key>|" or "<key>|activate"/"<key>|deactivate".
	activation := ""
	if i := indexByteFrom(clbk.Data, '|'); i >= 0 {
		activation = clbk.Data[i+1:]
	}
	switch activation {
	case "":
		if err := render(); err != nil {
			return err
		}
	case "activate":
		if err := spec.set(dbctx, userid, true); err != nil {
			return err
		}
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: spec.ansActivate})
		if err := render(); err != nil {
			return err
		}
	default:
		if err := spec.set(dbctx, userid, false); err != nil {
			return err
		}
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: spec.ansDeactivate})
		if err := render(); err != nil {
			return err
		}
	}
	return handlers.EndConversation()
}

func wppClbk(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.settingsToggle(tg, ctx, userid, toggleSpec{
		textKey: "settings/wpp", get: b.DB.GetWPP, set: b.DB.SetWPP,
		trueWord: txtWPPDefault, falseWord: txtWPPDisabled,
		activateBtn: settingsButtons["wpp-activate"], deactivateBtn: settingsButtons["wpp-deactivate"],
		ansActivate: txtWPPAnswerActivate, ansDeactivate: txtWPPAnswerDeactivate,
	})
}

func warningClbk(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.settingsToggle(tg, ctx, userid, toggleSpec{
		textKey: "settings/warning", get: b.DB.GetWarning, set: b.DB.SetWarning,
		trueWord: txtStateActive, falseWord: txtStateInactive,
		activateBtn: settingsButtons["warning-activate"], deactivateBtn: settingsButtons["warning-deactivate"],
		ansActivate: txtWarningAnswerActivate, ansDeactivate: txtWarningAnswerDeactivate,
	})
}

func seenSettings(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.settingsToggle(tg, ctx, userid, toggleSpec{
		textKey: "settings/seen", get: b.DB.GetSeenStatus, set: b.DB.SetSeenOption,
		trueWord: txtStateActive, falseWord: txtStateInactive,
		activateBtn: settingsButtons["seen-activate"], deactivateBtn: settingsButtons["seen-deactivate"],
		ansActivate: txtSeenAnswerActivate, ansDeactivate: txtSeenAnswerDeactivate,
	})
}

// changeName ports change_name: shows the rename prompt and enters state 0.
func changeName(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if ctx.CallbackQuery == nil || ctx.CallbackQuery.Data == "" {
		return nil
	}
	_, _ = ctx.CallbackQuery.Answer(tg, nil)
	dbctx, cancel := b.bg()
	defer cancel()
	name, err := b.DB.GetName(dbctx, userid)
	if err != nil {
		return err
	}
	txt, err := b.Texts.Get("settings/change_name")
	if err != nil {
		return err
	}
	if _, _, e := ctx.EffectiveMessage.EditText(tg, fmtText(txt, name), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: ikb(row(settingsButtons["formatting"]), row(settingsButtons["nvm-back-to-menu"])),
	}); e != nil {
		return e
	}
	b.ud(ctx).d.ogMID = ctx.EffectiveMessage.MessageId
	return handlers.NextConversationState("0")
}

// updateName ports update_name (state 0).
func updateName(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	newName := msg.OriginalHTML()
	if runeLen(newName) > b.Cfg.MaxNameLength {
		if e := b.sendPlain(ctx, fmt.Sprintf(
			"اسم جدید نباید بیشتر از %dتا حرف باشه. دوباره امتحان کن.\nنکته: قالب بندی یه مقدار به تعداد حروفت اضافه میکنه",
			b.Cfg.MaxNameLength)); e != nil {
			return e
		}
		return handlers.NextConversationState("0")
	}
	dbctx, cancel := b.bg()
	defer cancel()
	if err := b.DB.SetName(dbctx, userid, newName); err != nil {
		return err
	}
	b.deleteOgMID(tg, ctx, userid)
	name, err := b.DB.GetName(dbctx, userid)
	if err != nil {
		return err
	}
	if _, e := msg.Reply(tg, fmt.Sprintf(
		"انجام شد. اسم جدیدت:\n%s\n\nمیتونی لینک خودتو تست کنی تا ببینی چجوری شده :)", name),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: ikb(row(settingsButtons["back-to-menu"]))},
	); e != nil {
		// Python used reply_html here WITHOUT reply_parameters; gotgbot Reply
		// quotes, a harmless cosmetic difference accepted for the success notice.
		return e
	}
	return handlers.EndConversation()
}

// customTag ports custom_tag: shows the custom-tag prompt and enters state 1.
func customTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.tagPrompt(tg, ctx, userid, "settings/custom_tag", b.DB.GetCustomTag,
		settingsButtons["remove-custom-tag"], "1")
}

// audioTag ports audio_tag: shows the audio-tag prompt and enters state 2.
func audioTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.tagPrompt(tg, ctx, userid, "settings/audio_tag", b.DB.GetAudioTag,
		settingsButtons["remove-audio-tag"], "2")
}

func (b *Bot) tagPrompt(tg *gotgbot.Bot, ctx *ext.Context, userid, textKey string,
	get func(context.Context, string) (string, error), removeBtn gotgbot.InlineKeyboardButton, state string) error {
	if ctx.CallbackQuery == nil || ctx.CallbackQuery.Data == "" {
		return nil
	}
	_, _ = ctx.CallbackQuery.Answer(tg, nil)
	dbctx, cancel := b.bg()
	defer cancel()
	tag, err := get(dbctx, userid)
	if err != nil {
		return err
	}
	if tag == "" {
		tag = txtSettingsTagPlaceholder
	}
	txt, err := b.Texts.Get(textKey)
	if err != nil {
		return err
	}
	if _, _, e := ctx.EffectiveMessage.EditText(tg, fmtText(txt, tag), &gotgbot.EditMessageTextOpts{
		ParseMode: "HTML",
		ReplyMarkup: ikb(
			row(removeBtn),
			row(settingsButtons["formatting"]),
			row(settingsButtons["nvm-back-to-menu"]),
		),
	}); e != nil {
		return e
	}
	b.ud(ctx).d.ogMID = ctx.EffectiveMessage.MessageId
	return handlers.NextConversationState(state)
}

// updateTag is the shared body of update_custom_tag (state 1) / update_audio_tag
// (state 2).
func (b *Bot) updateTag(tg *gotgbot.Bot, ctx *ext.Context, userid string,
	set func(context.Context, string, *string) error, get func(context.Context, string) (string, error), state string) error {
	msg := ctx.EffectiveMessage
	newTag := msg.OriginalHTML()
	if runeLen(newTag) > b.Cfg.MaxNameLength {
		if _, e := msg.Reply(tg, fmt.Sprintf(
			"تگ جدید نباید بیشتر از %dتا حرف باشه. دوباره امتحان کن.\nنکته: لینک و بولد و اینا یه مقدار به تعداد حرفات اضافه میکنن",
			b.Cfg.MaxNameLength), nil); e != nil {
			return e
		}
		return handlers.NextConversationState(state)
	}
	dbctx, cancel := b.bg()
	defer cancel()
	if err := set(dbctx, userid, &newTag); err != nil {
		return err
	}
	b.deleteOgMID(tg, ctx, userid)
	tag, err := get(dbctx, userid)
	if err != nil {
		return err
	}
	if _, e := msg.Reply(tg, fmt.Sprintf(
		"انجام شد. تگ جدیدت:\n%s\n\nمیتونی لینک خودتو تست کنی تا ببینی چجوری شده :)", tag),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: ikb(row(settingsButtons["back-to-menu"]))},
	); e != nil {
		return e
	}
	return handlers.EndConversation()
}

func updateCustomTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.updateTag(tg, ctx, userid, b.DB.SetCustomTag, b.DB.GetCustomTag, "1")
}

func updateAudioTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.updateTag(tg, ctx, userid, b.DB.SetAudioTag, b.DB.GetAudioTag, "2")
}

// removeCustomTag / removeAudioTag port remove_custom_tag / remove_audio_tag:
// clear the tag (store NULL) and confirm in place.
func removeCustomTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.removeTag(tg, ctx, userid, b.DB.SetCustomTag)
}

func removeAudioTag(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	return b.removeTag(tg, ctx, userid, b.DB.SetAudioTag)
}

func (b *Bot) removeTag(tg *gotgbot.Bot, ctx *ext.Context, userid string,
	set func(context.Context, string, *string) error) error {
	dbctx, cancel := b.bg()
	defer cancel()
	if err := set(dbctx, userid, nil); err != nil {
		return err
	}
	if _, _, e := ctx.EffectiveMessage.EditText(tg, txtTagRemoved, &gotgbot.EditMessageTextOpts{
		ReplyMarkup: ikb(row(settingsButtons["back-to-menu"])),
	}); e != nil {
		return e
	}
	return handlers.EndConversation()
}

// unblockAll ports unblock_all_clbk.
func unblockAll(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)
	activation := ""
	if i := indexByteFrom(clbk.Data, '|'); i >= 0 {
		activation = clbk.Data[i+1:]
	}
	if activation != "" {
		dbctx, cancel := b.bg()
		defer cancel()
		if err := b.DB.UnblockAll(dbctx, userid); err != nil {
			return err
		}
		if _, _, e := ctx.EffectiveMessage.EditText(tg, txtUnblockAllDone, &gotgbot.EditMessageTextOpts{
			ReplyMarkup: ikb(row(settingsButtons["back-to-menu"])),
		}); e != nil {
			return e
		}
	} else {
		if _, _, e := ctx.EffectiveMessage.EditText(tg, txtUnblockAllConfirm, &gotgbot.EditMessageTextOpts{
			ParseMode: "HTML",
			ReplyMarkup: ikb(
				row(cb(btnUnblockAllSure, "unblock-all|yes")),
				row(settingsButtons["back-to-menu"]),
			),
		}); e != nil {
			return e
		}
	}
	return handlers.EndConversation()
}

// unblockMe ports unblock_me_clbk: sends the personal UNBLOCK link.
func unblockMe(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	intro, err := msg.Reply(tg, txtUnblockMeIntro, nil)
	if err != nil {
		return err
	}
	link := "t.me/" + b.TG.User.Username + "?start=UNBLOCK-" + userid
	_, err = msg.Reply(tg, link, &gotgbot.SendMessageOpts{
		ReplyParameters: &gotgbot.ReplyParameters{MessageId: intro.MessageId, AllowSendingWithoutReply: true},
	})
	return err
}

// whatIsFormatting ports what_is_formatting: a popup alert with the explanation.
func whatIsFormatting(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	if ctx.CallbackQuery == nil {
		return nil
	}
	txt, err := b.Texts.Get("formatting_explanation")
	if err != nil {
		return err
	}
	_, _ = ctx.CallbackQuery.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: txt, ShowAlert: true})
	return nil
}

// settingsCancelAll ports settings.cancel_all (the conversation fallback).
func settingsCancelAll(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	if e := b.replyText(ctx, txtSettingsCancelAll); e != nil {
		return e
	}
	b.ud(ctx).clear()
	return handlers.EndConversation()
}

// genericCancelCmd ports the /cancel fallback shared by settings and my_links.
func genericCancelCmd(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	b.ud(ctx).clear()
	if e := b.replyText(ctx, txtCancelledAllGeneric); e != nil {
		return e
	}
	return handlers.EndConversation()
}

// deleteOgMID deletes the stored settings menu message (best effort).
func (b *Bot) deleteOgMID(tg *gotgbot.Bot, ctx *ext.Context, _ string) {
	ud := b.ud(ctx)
	if ud.d.ogMID != 0 {
		_, _ = tg.DeleteMessage(ctx.EffectiveChat.Id, ud.d.ogMID, nil)
	}
}

// indexByteFrom returns the index of the first occurrence of c in s, or -1.
func indexByteFrom(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// runeLen counts code points, matching Python's len(str).
func runeLen(s string) int { return len([]rune(s)) }
