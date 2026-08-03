package bot

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// menuCallbackData is every callback_data the panel can emit: what the real
// keyboards build, plus every verb the handler switches on even if no keyboard
// shows it today.
func menuCallbackData() []string {
	var out []string
	for _, kb := range []gotgbot.InlineKeyboardMarkup{
		mainMenuKeyboard(false),
		mainMenuKeyboard(true),
		adminPanelKeyboard(),
	} {
		for _, r := range kb.InlineKeyboard {
			for _, btn := range r {
				out = append(out, btn.CallbackData)
			}
		}
	}
	for _, verb := range append(userMenuVerbs(), adminMenuVerbs()...) {
		out = append(out, "menu|"+verb)
	}
	return out
}

func userMenuVerbs() []string {
	return []string{menuMain, menuHelp, menuHelpText, menuPrivacy, menuMyUID, menuBug, menuDonate}
}

func adminMenuVerbs() []string {
	return []string{
		menuAdmin, menuAdminStats, menuAdminReports, menuAdminDonate,
		menuAdminDonOn, menuAdminDonOff, menuAdminBackup, menuAdminCount, menuAdminCmds,
	}
}

// conversationContainsFilters are the substrings the ConversationHandlers (and the
// standalone cqContains handlers) match on. A menu datum containing one would be
// swallowed by a conversation instead of reaching menuCallback — a button that
// silently does nothing.
var conversationContainsFilters = []string{
	"settings-menu", "what-is-formatting",
	"mylinks-menu", "what-is-cid", "add-link", "more-links",
	"rm-custom-tag", "rm-audio-tag", "anon-name-set", "anon-name-remove",
	"anon-name-noemoji",
}

// otherPrefixFilters are prefixes other handlers already claim.
var otherPrefixFilters = []string{
	"errmore|", "rpt|", "am|", "no-callback", "delete|",
	"reply-quote|", "media-settings|", "change-name|", "custom-tag|", "audio-tag|",
	"wpp|", "warning|", "easier-answer|", "channel-signature|", "seen-settings|",
	"anon-name|", "unblock-all|", "unblock-me|", "ch-link", "rm-link",
	"answer|", "seen|", "report|", "block|", "unblock|", "cancel",
}

// TestMenuDataDoesNotCollide is the guarantee that the panel's buttons actually
// reach the panel. This is the failure mode that would be invisible in review: the
// button renders, the tap goes to another handler, nothing happens.
func TestMenuDataDoesNotCollide(t *testing.T) {
	for _, data := range menuCallbackData() {
		if data == "" {
			t.Error("a menu button has empty callback_data")
			continue
		}

		// The two deliberate hand-offs into the existing conversations.
		if data == "settings-menu" || data == "mylinks-menu" {
			continue
		}

		if !strings.HasPrefix(data, "menu|") {
			t.Errorf("callback_data %q is neither a conversation entry point nor menu|-prefixed", data)
			continue
		}
		for _, sub := range conversationContainsFilters {
			if strings.Contains(data, sub) {
				t.Errorf("menu data %q contains %q, so a ConversationHandler would swallow it", data, sub)
			}
		}
		for _, p := range otherPrefixFilters {
			if strings.HasPrefix(data, p) {
				t.Errorf("menu data %q starts with %q, which another handler claims", data, p)
			}
		}
		if len(data) > 64 {
			t.Errorf("callback_data %q is %d bytes (>64)", data, len(data))
		}
	}
}

// TestMenuHandsOffToRealConversationEntryPoints pins the two buttons that must
// match the conversations' entry-point data EXACTLY. If either string drifted, the
// button would silently do nothing instead of opening links or settings.
func TestMenuHandsOffToRealConversationEntryPoints(t *testing.T) {
	kb := mainMenuKeyboard(false)
	if len(kb.InlineKeyboard) < 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("main menu's first row = %v; want two buttons", kb.InlineKeyboard)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "mylinks-menu" {
		t.Errorf("my-links button data = %q; want mylinks-menu (myLinksConversation's entry point)", got)
	}
	if got := kb.InlineKeyboard[0][1].CallbackData; got != "settings-menu" {
		t.Errorf("settings button data = %q; want settings-menu (settingsConversation's entry point)", got)
	}
}

// TestMenuAdminSectionIsAdminOnly checks the fifth section is hidden from
// non-admins AND gated in the handler — a hidden button is not a permission check.
func TestMenuAdminSectionIsAdminOnly(t *testing.T) {
	user := mainMenuKeyboard(false)
	for _, r := range user.InlineKeyboard {
		for _, btn := range r {
			if strings.Contains(btn.CallbackData, "menu|"+menuAdmin) {
				t.Errorf("non-admin menu exposes the admin section: %q", btn.CallbackData)
			}
		}
	}
	if got := countButtons(user); got != 4 {
		t.Errorf("non-admin menu has %d buttons; want 4", got)
	}
	if got := countButtons(mainMenuKeyboard(true)); got != 5 {
		t.Errorf("admin menu has %d buttons; want 5 (the 5th is the admin panel)", got)
	}

	// Every admin screen must be recognised by the gate. One missing from
	// isAdminVerb would be reachable by anyone who guessed its data.
	for _, verb := range adminMenuVerbs() {
		if !isAdminVerb(verb) {
			t.Errorf("isAdminVerb(%q) = false; that screen would be open to everyone", verb)
		}
	}
	// And no user-facing verb may be gated, which would lock users out of it.
	for _, verb := range userMenuVerbs() {
		if isAdminVerb(verb) {
			t.Errorf("isAdminVerb(%q) = true; ordinary users would be refused", verb)
		}
	}
}

// TestMenuEveryScreenIsReachable walks the panel like a user: from the main menu,
// following buttons, every screen must be arrived at, and every screen must offer a
// way back. A dead end is the classic menu bug.
func TestMenuEveryScreenIsReachable(t *testing.T) {
	// Screens reachable by following buttons from the two main menus and the admin
	// panel. The help sub-screen's buttons are built inline in the handler, so its
	// children are listed explicitly here — that list is what the handler renders.
	reachable := map[string]bool{}
	for _, kb := range []gotgbot.InlineKeyboardMarkup{mainMenuKeyboard(true), adminPanelKeyboard()} {
		for _, r := range kb.InlineKeyboard {
			for _, btn := range r {
				reachable[strings.TrimPrefix(btn.CallbackData, "menu|")] = true
			}
		}
	}
	// Children of the help screen.
	for _, v := range []string{menuHelpText, menuPrivacy, menuMyUID, menuBug} {
		reachable[v] = true
	}
	// Child of the donate-settings screen.
	reachable[menuAdminDonOn] = true
	reachable[menuAdminDonOff] = true

	for _, verb := range append(userMenuVerbs(), adminMenuVerbs()...) {
		if !reachable[verb] {
			t.Errorf("screen %q is handled but no button leads to it — dead code or a missing button", verb)
		}
	}

	// Back navigation must exist on the admin panel and every sub-screen keyboard
	// the helpers build.
	if !hasData(adminPanelKeyboard(), "menu|"+menuMain) {
		t.Error("the admin panel has no way back to the main menu")
	}
	if !hasDataInRow(backRow(), "menu|"+menuMain) {
		t.Error("backRow does not point at the main menu")
	}
}

func countButtons(kb gotgbot.InlineKeyboardMarkup) int {
	n := 0
	for _, r := range kb.InlineKeyboard {
		n += len(r)
	}
	return n
}

func hasData(kb gotgbot.InlineKeyboardMarkup, data string) bool {
	for _, r := range kb.InlineKeyboard {
		if hasDataInRow(r, data) {
			return true
		}
	}
	return false
}

func hasDataInRow(r []gotgbot.InlineKeyboardButton, data string) bool {
	for _, btn := range r {
		if btn.CallbackData == data {
			return true
		}
	}
	return false
}

// TestMenuBarFilter is the safety test for the bar. The tap arrives as an ordinary
// text message and the handler is registered ahead of the send state, so a filter
// that matched too much would swallow real messages — including somebody's
// anonymous message, which would be lost silently.
func TestMenuBarFilter(t *testing.T) {
	priv := func(text string) *gotgbot.Message {
		return &gotgbot.Message{Text: text, Chat: gotgbot.Chat{Type: "private"}}
	}

	if !menuBarFilter(priv(menuBarButton)) {
		t.Errorf("the bar's own label %q does not match its filter", menuBarButton)
	}
	// Telegram clients can pad the text; a tap must still register.
	if !menuBarFilter(priv("  " + menuBarButton + " ")) {
		t.Error("a padded bar tap did not match")
	}

	// Everything else must fall through to the normal handlers.
	for _, text := range []string{
		"", "منو", "🏠", "/menu", "سلام",
		menuBarButton + " چیه",    // the label inside a longer message
		"میخوام " + menuBarButton, // …and at the end
		"🏠 منوی اصلی",             // a near-miss label
	} {
		if menuBarFilter(priv(text)) {
			t.Errorf("filter claimed %q; a real message would be eaten", text)
		}
	}

	// Not a private chat, and a nil message, must never match.
	if menuBarFilter(&gotgbot.Message{Text: menuBarButton, Chat: gotgbot.Chat{Type: "supergroup"}}) {
		t.Error("the bar filter matched outside a private chat")
	}
	if menuBarFilter(nil) {
		t.Error("the bar filter matched a nil message")
	}
}

// TestMenuBarKeyboard pins the bar to ONE button — the whole point of the redesign
// was a single button, not a bar crowded with options.
func TestMenuBarKeyboard(t *testing.T) {
	kb := menuBarKeyboard()
	if len(kb.Keyboard) != 1 || len(kb.Keyboard[0]) != 1 {
		t.Fatalf("bar layout = %v; want exactly one button", kb.Keyboard)
	}
	if kb.Keyboard[0][0].Text != menuBarButton {
		t.Errorf("bar button = %q; want %q (must equal what the filter matches)",
			kb.Keyboard[0][0].Text, menuBarButton)
	}
	if !kb.IsPersistent {
		t.Error("the bar is not persistent, so it collapses behind the keyboard icon")
	}
	if !kb.ResizeKeyboard {
		t.Error("the bar is not resized, so it takes a full keyboard's height")
	}
}

// TestSettingsAndLinksCanReachTheMenu covers the gap that was reported: entering
// settings from the panel left no way back.
func TestSettingsAndLinksCanReachTheMenu(t *testing.T) {
	home := "menu|" + menuMain

	if !hasData(ikb(settingsMainMenu()...), home) {
		t.Error("the settings menu has no way back to /menu — a dead end")
	}
	if !hasData(ikb(mylinksDefaultMenu()...), home) {
		t.Error("the my_links menu has no way back to /menu — a dead end")
	}
}

// TestModerationScreensHaveAnExit checks the moderation list is not a dead end
// either, on both the /admin_reports path and the panel path.
func TestModerationScreensHaveAnExit(t *testing.T) {
	if modBackToPanel().CallbackData != "menu|"+menuAdmin {
		t.Errorf("the moderation exit points at %q; want the admin panel",
			modBackToPanel().CallbackData)
	}
}
