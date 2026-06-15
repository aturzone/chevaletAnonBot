package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// sendOutcome is the post-delete_notify_on_END result of send_msg_template:
// either re-enter the "composing" state (Python returned 0) or end the
// conversation (Python returned ConversationHandler.END).
type sendOutcome int

const (
	outcomeEnd sendOutcome = iota
	outcomeState0
)

// toState converts the outcome into the gotgbot conversation transition.
func (o sendOutcome) toState() error {
	if o == outcomeState0 {
		return handlers.NextConversationState(stateSending)
	}
	return handlers.EndConversation()
}

// sendMsg is the state-0 message handler (start.py send_msg). It is wrapped by
// delete_notify_on_END + prep_function in Python; the delete_notify_on_END
// behaviour lives inside sendMsgTemplate here.
func sendMsg(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, userid string) error {
	outcome, err := b.sendMsgTemplate(ctx, userid)
	if err != nil {
		return err
	}
	return outcome.toState()
}

// sendMsgTemplate is the delete_notify_on_END wrapper around the core send: it
// maps the del sentinels to a conversation outcome (del+0 -> state 0, del+e /
// normal -> END).
//
// Faithful subtlety: the Python delete_notify_on_END ALSO tried to delete the
// pre-send "جواب جدید:" notify (wrapper_list[0]) on failure — but
// send_msg_template's `context.user_data.clear()` runs BEFORE the notify is
// appended, and the append targets `user_data.get("wrapper_list", [])`, i.e. a
// throwaway list (the key was just cleared). So the decorator's wrapper_list is
// always empty and that deletion is dead code (the IndexError is swallowed). We
// reproduce that exactly: the notify is never deleted here.
func (b *Bot) sendMsgTemplate(ctx *ext.Context, userid string) (sendOutcome, error) {
	sentinel, err := b.sendMsgCore(ctx, userid)
	if err != nil {
		return outcomeEnd, err
	}
	if sentinel == del0 {
		return outcomeState0, nil
	}
	return outcomeEnd, nil
}

// sendMsgCore ports handler_templates.send_msg_template (the function body,
// before the decorator). It returns one of delNone(""), del0, delE: "" means a
// plain END (success or an early bail), del0/delE carry the handle_target_send
// sentinels up to the wrapper.
func (b *Bot) sendMsgCore(ctx *ext.Context, userid string) (string, error) {
	msg := ctx.EffectiveMessage
	ud := b.ud(ctx)

	targetChidPlain, _ := encoder.DecodeChevaletID(ud.d.targetChid)
	targetCid := ud.d.targetCid
	targetMid := ud.d.replyTo
	wasChannelReply := ud.d.channelReply
	externalReply := msg.ExternalReply
	mediaGroupID := msg.MediaGroupId
	ud.clear()

	dbctx, cancel := b.bg()
	defer cancel()

	// target uid
	targetUID, err := b.DB.GetUIDByChevaletID(dbctx, targetChidPlain)
	if err != nil {
		return "", err
	}
	if targetUID == "" {
		return delNone, b.replyHTML(ctx, txtSessionExpired, false)
	}

	// the target cid (if any) must still resolve to a user
	if targetCid != "" {
		uid, err := b.DB.GetUIDByCID(dbctx, targetCid)
		if err != nil {
			return "", err
		}
		if uid == "" {
			return delNone, b.replyHTML(ctx, txtContactChangedLink, false)
		}
	}

	// if the user pressed "answer" but replied to a DIFFERENT message, cancel.
	if targetMid != "" {
		kind, mChid, mMid, aerr := b.isAnswer(ctx, false)
		if aerr != nil {
			return "", aerr
		}
		if kind == answerMatch {
			mPlain, _ := encoder.DecodeChevaletID(mChid)
			if !(mPlain == targetChidPlain && mMid == targetMid) {
				return delNone, b.replyHTML(ctx, txtSendingToAnother, false)
			}
		}
	}

	// blocked by the target?
	blocked, err := b.DB.IsBlocked(dbctx, targetUID, userid)
	if err != nil {
		return "", err
	}
	if blocked {
		return delNone, b.replyText(ctx, txtBlockedYou)
	}

	// sender's encoded chevaletid (so the markup never leaks the uid/raw chid)
	senderChid, err := b.DB.GetChevaletIDByUID(dbctx, userid)
	if err != nil {
		return "", err
	}
	senderEncChid := encoder.EncodeChevaletID(senderChid)

	// reply / quote calculation (strings, like the Python original; converted to
	// int64 at the gotgbot boundary in copyToTarget).
	var replyToChatStr, replyToMidStr, quoteText string
	var quotePosition int64
	if externalReply != nil && externalReply.Chat != nil {
		replyToChatStr = strconv.FormatInt(externalReply.Chat.Id, 10)
		replyToMidStr = strconv.FormatInt(externalReply.MessageId, 10)
	} else if targetMid != "" {
		replyToChatStr = targetUID
		replyToMidStr = targetMid
	}
	if q := msg.Quote; q != nil {
		// Only honour a quote with no entities — Telegram rejects a quote that
		// carries formatting (Quote_text_invalid).
		if len(q.Entities) == 0 {
			quoteText = q.Text
			quotePosition = q.Position
		}
	}

	// if the bot is not an admin of the replied-to channel, fall back to an
	// inline link to that channel message instead of a real reply.
	repliedToLink := ""
	if externalReply != nil && externalReply.Chat != nil {
		if _, e := b.TG.GetChatAdministrators(externalReply.Chat.Id, nil); e != nil {
			if un := externalReply.Chat.Username; un != "" {
				repliedToLink = "https://t.me/" + un + "/" + replyToMidStr
			} else {
				// reply_to_chat[4:] strips the "-100" supergroup/channel prefix.
				repliedToLink = "https://t.me/c/" + replyToChatStr[4:] + "/" + replyToMidStr
			}
			replyToChatStr, replyToMidStr, quoteText, quotePosition = "", "", "", 0
		}
	}

	// keyboard: reply (+ optional seen), report/spacer/block.
	seen, err := b.DB.GetSeenStatus(dbctx, userid)
	if err != nil {
		return "", err
	}
	keyboard := messageKeyboard(senderEncChid, msg.MessageId, seen)

	targetCids, err := b.DB.GetCIDs(dbctx, targetUID)
	if err != nil {
		return "", err
	}

	var notifyMsg *gotgbot.Message
	if len(targetCids) > 1 && targetMid == "" {
		// a brand-new message (not an answer) to a user with several links:
		// show which link it came in through.
		if idx := indexOf(targetCids, targetCid); idx >= 0 {
			keyboard = append(keyboard, []gotgbot.InlineKeyboardButton{
				cb(fmt.Sprintf("ارسال شده با لینک %d (%s)", idx+1, targetCid), "no-callback"),
			})
		}
	} else if targetMid != "" && (externalReply != nil || (mediaGroupID != "" && externalReply == nil)) {
		// answering AND replied to an external/channel message (or a media-group
		// answer): send a "new answer:" notification first and reply to IT.
		nm, sentinel, nerr := b.sendNotif(ctx, targetUID, targetMid, externalReply != nil)
		if nerr != nil {
			return "", nerr
		}
		if sentinel != delNone {
			// FIX: the Python original used notify_msg.message_id unconditionally
			// right after this send and would crash (AttributeError) if the notify
			// was rejected. Honour the del sentinel here instead.
			return sentinel, nil
		}
		notifyMsg = nm
		replyToChatStr = ""
		replyToMidStr = strconv.FormatInt(nm.MessageId, 10)
		quoteText, quotePosition = "", 0
		// (Python appended notify_msg to a throwaway wrapper_list here; see the
		// note in sendMsgTemplate — the decorator's deletion is dead code.)
	}

	// trailing donation row
	keyboard = append(keyboard, donationRow(b.Cfg.DonationLink))
	replyMarkup := gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard}

	// media groups: stash everything and wait for the rest of the group.
	if mediaGroupID != "" {
		ud.d.mediaGroupID = mediaGroupID
		ud.d.groupMsgs = []*gotgbot.Message{msg}
		ud.d.groupExpiration = nowSeconds() + config.ExpireAfter
		// target_chid / reply_to / channel_reply were reset by ud.clear() above.
		ud.d.groupTargetChid = encoder.EncodeChevaletID(targetChidPlain)
		ud.d.groupWasChannelReply = wasChannelReply
		ud.d.groupNotifyMsg = notifyMsg
		ud.d.groupReplyMarkup = &replyMarkup
		ud.d.groupWarningMsgID = ""
		return delNone, nil // END
	}

	targetID, err := strconv.ParseInt(targetUID, 10, 64)
	if err != nil {
		return "", err
	}

	// copy the message to the target.
	copiedID, sentinel, err := b.copyToTarget(ctx, targetID, &replyMarkup,
		replyToChatStr, replyToMidStr, quoteText, quotePosition, externalReply != nil)
	if err != nil {
		return "", err
	}
	if sentinel != delNone {
		ud.clear()
		return sentinel, nil
	}

	// remove the link preview on the copy if the target disabled wpp.
	wpp, err := b.DB.GetWPP(dbctx, targetUID)
	if err != nil {
		return "", err
	}
	if !wpp && msg.Text != "" {
		_, _, _ = b.TG.EditMessageText(msg.OriginalHTML(), &gotgbot.EditMessageTextOpts{
			ChatId:             targetID,
			MessageId:          copiedID,
			ParseMode:          "HTML",
			ReplyMarkup:        replyMarkup,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		}) // errors ignored, like the Python try/except pass
	}

	// "sent" + the deletion warning. The callback data carries a fresh encoding
	// of the target chid plus the copied (and notify) message ids; when notify is
	// nil the Python f-string produced the literal "None", which we reproduce.
	notifyPart := "None"
	if notifyMsg != nil {
		notifyPart = strconv.FormatInt(notifyMsg.MessageId, 10)
	}
	deletionCallbackData := encoder.EncodeChevaletID(targetChidPlain) + "|" +
		strconv.FormatInt(copiedID, 10) + "|" + notifyPart
	if _, err := b.warningHandle(ctx, wasChannelReply, targetUID, userid, deletionCallbackData); err != nil {
		return "", err
	}

	// reply tag, custom tag, audio tag.
	customTag, err := b.DB.GetCustomTag(dbctx, targetUID)
	if err != nil {
		return "", err
	}
	if repliedToLink != "" {
		repliedToLink = `<blockquote><a href="` + repliedToLink + `">ریپلای به این پیام</a></blockquote>`
	}
	switch {
	case msg.Audio != nil && customTag == "":
		audioTag, err := b.DB.GetAudioTag(dbctx, targetUID)
		if err != nil {
			return "", err
		}
		b.addTag(msg, "caption", targetID, copiedID, replyMarkup, audioTag+"\n"+repliedToLink)
	case customTag != "":
		if !b.addTag(msg, "text", targetID, copiedID, replyMarkup, customTag+"\n"+repliedToLink) {
			b.addTag(msg, "caption", targetID, copiedID, replyMarkup, customTag+"\n"+repliedToLink)
		}
	case repliedToLink != "":
		if !b.addTag(msg, "text", targetID, copiedID, replyMarkup, repliedToLink) {
			b.addTag(msg, "caption", targetID, copiedID, replyMarkup, repliedToLink)
		}
	}

	ud.clear()
	return delNone, nil // END
}

// sendNotif ports the @handle_target_send send_notif closure: a "new answer:"
// message to the target, replying to target_mid. Returns the message, or a del
// sentinel when the send was rejected.
func (b *Bot) sendNotif(ctx *ext.Context, targetUID, targetMid string, externalReply bool) (*gotgbot.Message, string, error) {
	tid, err := strconv.ParseInt(targetUID, 10, 64)
	if err != nil {
		return nil, delNone, err
	}
	var opts *gotgbot.SendMessageOpts
	if targetMid != "" {
		mid, _ := strconv.ParseInt(targetMid, 10, 64)
		opts = &gotgbot.SendMessageOpts{ReplyParameters: &gotgbot.ReplyParameters{MessageId: mid}}
	}
	nm, serr := b.TG.SendMessage(tid, txtNotifNewAnswer, opts)
	if serr != nil {
		sentinel, perr := b.classifyTargetSendErr(ctx, externalReply, serr)
		return nil, sentinel, perr
	}
	return nm, delNone, nil
}

// copyToTarget ports the @handle_target_send copy_msg_to_target closure. Returns
// the copied message id, or a del sentinel when the copy was rejected.
func (b *Bot) copyToTarget(ctx *ext.Context, targetID int64, markup *gotgbot.InlineKeyboardMarkup,
	replyChatStr, replyMidStr, quoteText string, quotePos int64, externalReply bool) (int64, string, error) {

	opts := &gotgbot.CopyMessageOpts{ParseMode: "HTML", ReplyMarkup: *markup}
	if replyMidStr != "" {
		rp := &gotgbot.ReplyParameters{}
		rp.MessageId, _ = strconv.ParseInt(replyMidStr, 10, 64)
		if replyChatStr != "" {
			rp.ChatId, _ = strconv.ParseInt(replyChatStr, 10, 64)
		}
		if quoteText != "" {
			rp.Quote = quoteText
			rp.QuotePosition = quotePos
		}
		opts.ReplyParameters = rp
	}
	mi, cerr := ctx.EffectiveMessage.Copy(b.TG, targetID, opts)
	if cerr != nil {
		sentinel, perr := b.classifyTargetSendErr(ctx, externalReply, cerr)
		return 0, sentinel, perr
	}
	return mi.MessageId, delNone, nil
}

// warningHandle ports handler_templates._warning_handle: it sends the "sent"
// confirmation, and (when warnings are on, or it was a channel reply) attaches
// "delete" buttons and schedules the countdown auto-edit. Returns the warning
// message (or nil when no warning was shown).
func (b *Bot) warningHandle(ctx *ext.Context, wasChannelReply bool, targetUID, userid, deletionCallbackData string) (*gotgbot.Message, error) {
	msg := ctx.EffectiveMessage
	dbctx, cancel := b.bg()
	defer cancel()

	deletionTimeout := config.DeletionTimeout
	var sentText string
	if wasChannelReply {
		deletionTimeout = config.DeletionTimeoutExtended
		name, err := b.DB.GetName(dbctx, targetUID)
		if err != nil {
			return nil, err
		}
		// html.escape on the bug text is a no-op (it has no & < > " '), so we
		// embed it verbatim. The name is NOT escaped, matching the original.
		sentText = fmt.Sprintf("فرستادم به %s.\n<blockquote><b>%s</b></blockquote>\n", name, txtWarnBugInner)
	} else {
		sentText = txtSentToThem
	}

	showWarning := wasChannelReply
	if !showWarning {
		w, err := b.DB.GetWarning(dbctx, userid)
		if err != nil {
			return nil, err
		}
		showWarning = w
	}

	if !showWarning {
		return nil, b.replyText(ctx, sentText)
	}

	// pack the message ids into as few "delete" buttons as fit in 64 bytes.
	parts := strings.Split(deletionCallbackData, "|")
	encChid := parts[0]
	mids := parts[1:]
	kbTemplate := func() []string { return []string{"delete", encChid} }
	isValid := func(rm []string) bool { return len(strings.Join(rm, "|")) <= 64 }

	var buttons []gotgbot.InlineKeyboardButton
	tempKB := kbTemplate()
	for _, mid := range mids {
		candidate := append(append([]string{}, tempKB...), mid)
		if isValid(candidate) {
			tempKB = candidate
		} else {
			buttons = append(buttons, cb(btnDelete, strings.Join(tempKB, "|")))
			tempKB = append(kbTemplate(), mid)
		}
	}
	if !equalStringSlices(tempKB, kbTemplate()) {
		buttons = append(buttons, cb(btnDelete, strings.Join(tempKB, "|")))
	}

	var rows [][]gotgbot.InlineKeyboardButton
	for i := 0; i < len(buttons); i += 2 {
		end := i + 2
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, buttons[i:end])
	}

	// Python applied `% deletion_timeout` to the whole "{sent_text}\n{DELETION_TEXT}"
	// string; we only format DELETION_TEXT so a literal '%' in the target's
	// display name can never break the formatting.
	warnText := sentText + "\n" + fmt.Sprintf(config.DeletionText, deletionTimeout)
	warnMsg, err := msg.Reply(b.TG, warnText, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		return nil, err
	}
	b.scheduleDeleteWarning(warnMsg, deletionTimeout)
	return warnMsg, nil
}

// addTag ports handler_templates.add_tag: it appends a tag to the copied
// message's text or caption (edit_what is "text" or "caption"). It returns true
// on success and false on any error (Python returned True / None).
func (b *Bot) addTag(msg *gotgbot.Message, editWhat string, targetID, copiedID int64, markup gotgbot.InlineKeyboardMarkup, tag string) bool {
	if editWhat == "caption" {
		caption := msg.OriginalCaptionHTML() + "\n" + tag
		_, _, err := b.TG.EditMessageCaption(&gotgbot.EditMessageCaptionOpts{
			Caption:               caption,
			ChatId:                targetID,
			MessageId:             copiedID,
			ParseMode:             "HTML",
			ReplyMarkup:           markup,
			ShowCaptionAboveMedia: msg.ShowCaptionAboveMedia,
		})
		return err == nil
	}
	text := msg.OriginalHTML() + "\n" + tag
	_, _, err := b.TG.EditMessageText(text, &gotgbot.EditMessageTextOpts{
		ChatId:             targetID,
		MessageId:          copiedID,
		ParseMode:          "HTML",
		ReplyMarkup:        markup,
		LinkPreviewOptions: msg.LinkPreviewOptions,
	})
	return err == nil
}
