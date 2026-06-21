package bot

import (
	"log/slog"
	"sort"
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// handleMedia ports start.handle_media: the top-level handler for media-group
// messages. The first media of a group is handled by send_msg/send_msg_template
// (which stashes the group state); each subsequent media of the same group lands
// here, where the whole group is re-copied to the target and re-tagged.
func handleMedia(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	ud := b.ud(ctx)

	slog.Info("handleMedia", "kind", kindOf(msg), "groupId", msg.MediaGroupId,
		"haveGroup", len(ud.d.groupMsgs) > 0, "match", msg.MediaGroupId == ud.d.mediaGroupID)

	// Not part of a tracked group (or a different group) -> treat as a fresh
	// auto-reply attempt, else "didn't understand".
	if len(ud.d.groupMsgs) == 0 || msg.MediaGroupId != ud.d.mediaGroupID {
		ud.clear()
		handled, err := b.checkIfAutoreply(ctx, userid)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		return b.otherMessagesTemplate(ctx)
	}

	// Defensive: group state must be intact.
	if ud.d.groupExpiration == 0 || len(ud.d.groupMsgs) == 0 {
		return b.otherMessagesTemplate(ctx)
	}

	// Expired window -> ask the user to start fresh. Note the Python comparison
	// uses an already-future expiration (set to now+EXPIRE_AFTER), reproduced in
	// nowSeconds()/groupExpiration verbatim.
	if nowSeconds()-ud.d.groupExpiration >= config.ExpireAfter {
		ud.clear()
		// reply quoting this message in the user's own chat.
		_, err := msg.Reply(tg, txtTooLate, &gotgbot.SendMessageOpts{
			ReplyParameters: &gotgbot.ReplyParameters{
				MessageId: msg.MessageId,
				ChatId:    ctx.EffectiveChat.Id,
			},
		})
		return err
	}

	// add this media to the group
	ud.d.groupMsgs = append(ud.d.groupMsgs, msg)

	encodedTargetChid := ud.d.groupTargetChid
	targetChid, _ := encoder.DecodeChevaletID(encodedTargetChid)

	dbctx, cancel := b.bg()
	defer cancel()

	targetUID, err := b.DB.GetUIDByChevaletID(dbctx, targetChid)
	if err != nil {
		return err
	}
	targetID, err := strconv.ParseInt(targetUID, 10, 64)
	if err != nil {
		return err
	}
	userID64, err := strconv.ParseInt(userid, 10, 64)
	if err != nil {
		return err
	}
	// Album items arrive as separate, concurrently-dispatched updates; prep
	// serialises them per-user but NOT in message-id order (whichever update grabs
	// the lock first is appended first), and Telegram may even redeliver one. So
	// sort + dedupe by message id before copying — tg.CopyMessages REQUIRES the
	// ids in strictly-increasing order (else "message identifiers must be in a
	// strictly increasing order").
	msgs := dedupeSortMsgs(ud.d.groupMsgs)
	ud.d.groupMsgs = msgs
	replyMarkup := *ud.d.groupReplyMarkup

	// delete the previously-copied set, then re-copy the whole group.
	if len(ud.d.sentMedias) > 0 {
		if ids, ok := parseIDs(ud.d.sentMedias); ok {
			_, _ = tg.DeleteMessages(targetID, ids, nil)
		}
		ud.d.sentMedias = nil
	}
	srcIDs := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		srcIDs = append(srcIDs, m.MessageId)
	}
	sentMessages, err := tg.CopyMessages(targetID, userID64, srcIDs, nil)
	if err != nil {
		return err
	}
	for _, sm := range sentMessages {
		ud.d.sentMedias = append(ud.d.sentMedias, strconv.FormatInt(sm.MessageId, 10))
	}
	slog.Info("media group copied to target", "type", mediaType(msgs[0]), "items", len(sentMessages))

	// decide which message(s) to tag and with which tag.
	mt := mediaType(msgs[0])
	var tag string
	if mt == "audio" {
		if tag, err = b.DB.GetAudioTag(dbctx, targetUID); err != nil {
			return err
		}
	} else {
		if tag, err = b.DB.GetCustomTag(dbctx, targetUID); err != nil {
			return err
		}
	}

	// Parity with Python handle_media: it passes the raw tag straight to add_tag,
	// and when the target has no custom tag (NULL -> Python None) the
	// `og_text_html + "\n" + tag` concatenation raises a TypeError that add_tag
	// swallows — so the album captions are left UNTOUCHED and no reply markup is
	// placed on any media item; the keyboard reaches the target only via the
	// carrier message below. Go's GetCustomTag maps NULL -> "", so we reproduce
	// that no-op by tagging only when a tag actually exists. (audio_tag has a
	// non-null default, so audio albums are always tagged, matching Python.)
	if tag != "" {
		type tagTarget struct {
			idx int
			msg *gotgbot.Message
		}
		markAll := func() []tagTarget {
			all := make([]tagTarget, 0, len(msgs))
			for i, m := range msgs {
				all = append(all, tagTarget{idx: i, msg: m})
			}
			return all
		}
		var toTag []tagTarget
		if mt == "photo" || mt == "video" {
			for i, m := range msgs {
				if m.OriginalCaptionHTML() != "" {
					if len(toTag) > 0 {
						// more than one captioned message -> tag them all.
						toTag = markAll()
						break
					}
					toTag = append(toTag, tagTarget{idx: i, msg: m})
				}
			}
			if len(toTag) == 0 {
				toTag = append(toTag, tagTarget{idx: 0, msg: msgs[0]})
			}
		} else {
			toTag = markAll()
		}
		for _, tt := range toTag {
			if tt.idx < len(ud.d.sentMedias) {
				copiedID, _ := strconv.ParseInt(ud.d.sentMedias[tt.idx], 10, 64)
				b.addTag(tt.msg, "caption", targetID, copiedID, replyMarkup, sanitizeUserHTML(tag))
			}
		}
	}

	// send the reply-markup carrier message (replying to the first media).
	markupMsg, err := tg.SendMessage(targetID, txtMediaUseThis, &gotgbot.SendMessageOpts{
		ParseMode:       "HTML",
		ReplyParameters: &gotgbot.ReplyParameters{MessageId: sentMessages[0].MessageId, ChatId: targetID},
		ReplyMarkup:     replyMarkup,
	})
	if err != nil {
		return err
	}
	ud.d.sentMedias = append(ud.d.sentMedias, strconv.FormatInt(markupMsg.MessageId, 10))

	// delete the previous warning message (it referenced the now-stale copies).
	if ud.d.groupWarningMsgID != "" {
		if wid, e := strconv.ParseInt(ud.d.groupWarningMsgID, 10, 64); e == nil {
			_, _ = tg.DeleteMessage(userID64, wid, nil)
		}
	}

	// build the deletion id list (sent medias + notify), de-duplicated in order.
	tbd := append([]string{}, ud.d.sentMedias...)
	if ud.d.groupNotifyMsg != nil {
		tbd = append(tbd, strconv.FormatInt(ud.d.groupNotifyMsg.MessageId, 10))
	}
	tbd = dedupeOrdered(tbd)

	deletionCallbackData := encodedTargetChid
	for _, m := range tbd {
		deletionCallbackData += "|" + m
	}
	warnMsg, err := b.warningHandle(ctx, ud.d.groupWasChannelReply, targetUID, userid, deletionCallbackData)
	if err != nil {
		return err
	}
	if warnMsg != nil {
		ud.d.groupWarningMsgID = strconv.FormatInt(warnMsg.MessageId, 10)
	}
	ud.d.groupExpiration = nowSeconds() + config.ExpireAfter
	return nil
}

// kindOf describes a message's media kind for diagnostic logging.
func kindOf(m *gotgbot.Message) string {
	switch {
	case m.Audio != nil:
		return "audio"
	case m.Voice != nil:
		return "voice"
	case len(m.Photo) > 0:
		return "photo"
	case m.Video != nil:
		return "video"
	case m.Document != nil:
		return "document"
	case m.Animation != nil:
		return "animation"
	case m.VideoNote != nil:
		return "video_note"
	case m.Sticker != nil:
		return "sticker"
	case m.Text != "":
		return "text"
	default:
		return "other"
	}
}

// mediaType returns the bot-relevant message type of m, mirroring the slices of
// telegram.helpers.effective_message_type that handle_media branches on.
func mediaType(m *gotgbot.Message) string {
	switch {
	case m.Audio != nil:
		return "audio"
	case len(m.Photo) > 0:
		return "photo"
	case m.Video != nil:
		return "video"
	default:
		return "other"
	}
}

// parseIDs converts a slice of decimal id strings to int64s; ok is false if any
// fail to parse.
func parseIDs(ss []string) ([]int64, bool) {
	out := make([]int64, 0, len(ss))
	for _, s := range ss {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// dedupeSortMsgs returns the messages sorted by ascending message id with
// duplicate ids removed — the order tg.CopyMessages requires. Album items arrive
// as concurrently-dispatched updates, so the accumulated slice can be unordered
// or hold a redelivered duplicate.
func dedupeSortMsgs(in []*gotgbot.Message) []*gotgbot.Message {
	out := make([]*gotgbot.Message, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].MessageId < out[j].MessageId })
	dedup := out[:0]
	last := int64(-1)
	for _, m := range out {
		if m.MessageId == last {
			continue
		}
		dedup = append(dedup, m)
		last = m.MessageId
	}
	return dedup
}

// dedupeOrdered removes duplicates while preserving first-seen order.
func dedupeOrdered(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, x := range in {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
