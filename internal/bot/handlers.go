package bot

import (
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/conversation"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters"
	cqfilters "github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	msgfilters "github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

// registerHandlers wires the dispatcher in the same group-0 order as the Python
// main.py handler list, so exactly one handler processes each update and
// precedence matches the original (the conversation outranks the media-group and
// catch-all handlers; the catch-all is last).
//
// Phase 3 adds: no-callback, the /start conversation (start/answer/seen/block/
// unblock/report/delete callbacks + the state-0 send_msg + fallbacks), the
// media-group handler and the top-level delete handler, plus the other_messages
// catch-all. Phases 4/5 (settings, my_links, admin, AI, jobs) come later.
func (b *Bot) registerHandlers() {
	d := b.Dispatcher

	// no_callback_handler — answers the spacer / "sent with link" buttons.
	d.AddHandler(handlers.NewCallback(cqfilters.Prefix("no-callback"), b.topLevel(noCallback)))

	// start_cmd_handler — the per-user conversation.
	d.AddHandler(b.startConversation())

	// media_group_handler — subsequent media of a group.
	d.AddHandler(handlers.NewMessage(msgfilters.MediaGroup, b.topLevel(handleMedia)))

	// delete_message_handler — the warning's "delete" button, standalone.
	d.AddHandler(handlers.NewCallback(cqfilters.Prefix("delete|"), b.topLevel(deleteMsgClbk)))

	// Phase 2 self-contained commands.
	b.command("help", cmdHelp)
	b.command("privacy", cmdPrivacy)
	b.command("donate", cmdDonate)
	b.command("myuid", cmdMyUID)
	b.command("bug", cmdBug)

	// other_messages_handler — the catch-all, registered LAST so the conversation
	// and media handlers take precedence. The Python `& ~IsDirectMessageFilter`
	// (exclude business "direct messages" chats) is unnecessary here because prep
	// already restricts handling to private chats.
	d.AddHandler(handlers.NewMessage(msgfilters.All, b.topLevel(otherMessages)))
}

// startConversation builds the ConversationHandler ported from start.py
// (start_cmd_handler): per-user, entry points + state "0" (composing) +
// fallbacks. Entry points and state-0 share the same callback set; state 0 adds
// the send_msg message handler.
func (b *Bot) startConversation() handlers.Conversation {
	callbacks := func() []ext.Handler {
		return []ext.Handler{
			handlers.NewCommand("start", b.prep(startCmd)),
			handlers.NewCallback(cqfilters.Prefix("answer|"), b.prep(answer)),
			handlers.NewCallback(cqfilters.Prefix("seen|"), b.prep(seen)),
			handlers.NewCallback(cqContains("alread-seen"), b.prep(alreadySeen)),
			handlers.NewCallback(cqfilters.Prefix("report|"), b.prep(report)),
			handlers.NewCallback(cqfilters.Prefix("report_yes|"), b.prep(reportConfirmYes)),
			handlers.NewCallback(cqfilters.Prefix("report_no"), b.prep(reportConfirmNo)),
			handlers.NewCallback(cqfilters.Prefix("block|"), b.prep(block)),
			handlers.NewCallback(cqfilters.Prefix("unblock|"), b.prep(unblock)),
		}
	}

	entryPoints := callbacks()
	state0 := append(callbacks(),
		handlers.NewMessage(notCommand, b.prep(sendMsg)),
	)

	return handlers.NewConversation(
		entryPoints,
		map[string][]ext.Handler{stateSending: state0},
		&handlers.ConversationOpts{
			Fallbacks: []ext.Handler{
				handlers.NewCallback(cqfilters.Prefix("delete|"), b.prep(deleteMsgClbk)),
				handlers.NewCallback(cqContains("cancel"), b.prep(cancel)),
				handlers.NewCommand("cancel", b.prep(cancelCmd)),
				handlers.NewMessage(msgfilters.All, b.prep(cancelAll)),
			},
			StateStorage: conversation.NewInMemoryStorage(conversation.KeyStrategySender),
		},
	)
}

// notCommand mirrors PTB's `filters.ALL & ~filters.COMMAND` for the state-0
// send_msg handler: any message that is not a bot command.
func notCommand(m *gotgbot.Message) bool { return !msgfilters.Command(m) }

// cqContains builds a callback-query filter matching data that CONTAINS sub,
// reproducing the Python CallbackQueryHandler patterns that used an unanchored
// regex (e.g. r"cancel", r"alread-seen").
func cqContains(sub string) filters.CallbackQuery {
	return func(cq *gotgbot.CallbackQuery) bool {
		return strings.Contains(cq.Data, sub)
	}
}

// replyHTML replies to the triggering message in HTML, optionally disabling the
// link preview (the Python handlers pass disable_web_page_preview=True).
func (b *Bot) replyHTML(ctx *ext.Context, text string, disablePreview bool) error {
	opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if disablePreview {
		opts.LinkPreviewOptions = &gotgbot.LinkPreviewOptions{IsDisabled: true}
	}
	_, err := ctx.EffectiveMessage.Reply(b.TG, text, opts)
	return err
}

// cmdHelp mirrors help.help_cmd: the start/help text with the donation link.
func cmdHelp(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	txt, err := b.Texts.Get("start_help")
	if err != nil {
		return err
	}
	txt = strings.ReplaceAll(txt, "%s", b.Cfg.DonationLink)
	return b.replyHTML(ctx, txt, true)
}

// cmdPrivacy mirrors privacy.privacy_cmd: the privacy/safety text.
func cmdPrivacy(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	txt, err := b.Texts.Get("privacy_safety")
	if err != nil {
		return err
	}
	return b.replyHTML(ctx, txt, true)
}

// cmdDonate mirrors donate.donate_cmd: the donation text with the donation link.
func cmdDonate(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	txt, err := b.Texts.Get("donate")
	if err != nil {
		return err
	}
	txt = strings.ReplaceAll(txt, "%s", b.Cfg.DonationLink)
	return b.replyHTML(ctx, txt, true)
}

// cmdMyUID mirrors myuid.myuid_cmd: replies with the user's Telegram id.
func cmdMyUID(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, userid string) error {
	_, err := ctx.EffectiveMessage.Reply(b.TG, userid, nil)
	return err
}

// cmdBug mirrors warn_bug.warn_bug_reply_to_channel: the channel-reply warning,
// with a small-media link preview pointing at the relevant Telegram bug.
func cmdBug(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	txt, err := b.Texts.Get("warn_reply_to_channel")
	if err != nil {
		return err
	}
	_, err = ctx.EffectiveMessage.Reply(b.TG, txt, &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
			Url:              "https://bugs.telegram.org/c/47222",
			PreferSmallMedia: true,
		},
	})
	return err
}
