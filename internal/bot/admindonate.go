package bot

import (
	"errors"
	"html"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// The donation button under delivered messages was removed by request, and this
// is how an admin brings it back without a redeploy. Both the on/off flag and the
// URL live in dynamic_settings.json (see internal/dynset), so a restart keeps
// whatever was set.
//
// Reachable two ways on purpose: `/admin donate …` matches every other admin
// sub-command, and `/admin_donate …` is the spelling that was asked for. They run
// the same code.
//
//	/admin_donate                 -> show the current state
//	/admin_donate active          -> show the button
//	/admin_donate deactive        -> hide the button (the default)
//	/admin_donate link <url>      -> set the donation URL
//	/admin_donate link reset      -> go back to the configured DONATION_LINK

// donateKind is what the admin asked for.
type donateKind int

const (
	donateShow      donateKind = iota // no args: just report the state
	donateToggle                      // active / deactive
	donateSetLink                     // link <url>
	donateResetLink                   // link reset
)

// donateAction is the parsed request. Keeping parsing separate from Telegram I/O
// makes the accepted spellings and the URL rule unit-testable.
type donateAction struct {
	kind donateKind
	on   bool   // for donateToggle
	link string // for donateSetLink
}

// errBadDonateURL rejects a link Telegram would refuse as a URL button target.
// This matters more than cosmetics: an invalid url makes EVERY delivered message
// fail to send, so it must never reach the keyboard.
var errBadDonateURL = errors.New("donation link must start with http:// or https://")

// parseDonateArgs turns the admin's words into an action.
//
// "active"/"deactive" is the wording from the request; "activate"/"deactivate",
// "enable"/"disable" and "on"/"off" are accepted too, because those are what
// people actually type. A leading "donate" is dropped so /admin donate … and
// /admin_donate … can share this.
func parseDonateArgs(args []string) (donateAction, error) {
	if len(args) > 0 && strings.EqualFold(args[0], "donate") {
		args = args[1:]
	}
	if len(args) == 0 {
		return donateAction{kind: donateShow}, nil
	}

	switch strings.ToLower(args[0]) {
	case "active", "activate", "enable", "on":
		return donateAction{kind: donateToggle, on: true}, nil

	case "deactive", "deactivate", "disable", "off":
		return donateAction{kind: donateToggle, on: false}, nil

	case "link", "set":
		v, err := at(args, 1)
		if err != nil {
			return donateAction{}, err
		}
		if strings.EqualFold(v, "reset") {
			return donateAction{kind: donateResetLink}, nil
		}
		if !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			return donateAction{}, errBadDonateURL
		}
		return donateAction{kind: donateSetLink, link: v}, nil

	default:
		return donateAction{}, errWrongSyntax
	}
}

// adminDonate applies a parsed request and always replies with the resulting
// state, so the admin sees what is live rather than only that something changed.
func (b *Bot) adminDonate(ctx *ext.Context, args []string) error {
	act, err := parseDonateArgs(args)
	switch {
	case errors.Is(err, errBadDonateURL):
		return b.replyHTML(ctx, "the link must start with <code>https://</code> or <code>http://</code>.", false)
	case err != nil:
		return err // errWrongSyntax -> the caller's usage reply
	}

	switch act.kind {
	case donateShow:
		return b.replyHTML(ctx, b.donateStatus(), false)

	case donateToggle:
		b.Dyn.SetDonationEnabled(act.on)
		head := "❌ donation button DEACTIVE."
		if act.on {
			head = "✅ donation button ACTIVE."
		}
		return b.replyHTML(ctx, head+"\n\n"+b.donateStatus(), false)

	case donateSetLink:
		b.Dyn.SetDonationLink(act.link)
		return b.replyHTML(ctx, "donation link updated.\n\n"+b.donateStatus(), false)

	case donateResetLink:
		b.Dyn.ResetDonationLink()
		return b.replyHTML(ctx, "donation link reset to the configured default.\n\n"+b.donateStatus(), false)
	}
	return errWrongSyntax
}

// donateStatus renders the current flag and URL.
func (b *Bot) donateStatus() string {
	state := "❌ deactive (button hidden)"
	if b.Dyn.DonationEnabled() {
		state = "✅ active (button shown)"
	}
	return "donation button: " + state +
		"\nlink: <code>" + html.EscapeString(b.Dyn.DonationLink()) + "</code>" +
		"\n\n<code>/admin_donate active|deactive</code>" +
		"\n<code>/admin_donate link &lt;url&gt;</code>"
}

// adminDonateCmd backs the /admin_donate alias.
func adminDonateCmd(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if !b.isAdmin(userid) {
		// Same as /admin: a non-admin must not learn the command exists.
		return b.otherMessagesTemplate(ctx)
	}
	fields := strings.Fields(ctx.EffectiveMessage.Text)
	if len(fields) > 0 {
		fields = fields[1:] // drop "/admin_donate"
	}
	if err := b.adminDonate(ctx, fields); errors.Is(err, errWrongSyntax) {
		return b.replyHTML(ctx, "wrong syntax.\n\n"+b.donateStatus(), false)
	} else if err != nil {
		return err
	}
	return nil
}
