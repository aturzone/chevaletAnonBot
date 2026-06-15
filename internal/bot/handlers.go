package bot

import (
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// registerHandlers wires the dispatcher. Phase 2 covers the self-contained
// text commands; Phase 3+ adds /start (+conversation), settings, my_links,
// admin, the reply/seen/block/report/delete callbacks, media groups, AI chat
// and the background jobs.
func (b *Bot) registerHandlers() {
	b.command("help", cmdHelp)
	b.command("privacy", cmdPrivacy)
	b.command("donate", cmdDonate)
	b.command("myuid", cmdMyUID)
	b.command("bug", cmdBug)
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
