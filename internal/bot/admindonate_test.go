package bot

import (
	"errors"
	"testing"
)

// TestParseDonateArgs locks the /admin_donate grammar: which words toggle the
// button, that a leading "donate" is dropped so /admin donate … and
// /admin_donate … behave identically, and that a link Telegram would reject never
// gets through (an invalid URL button makes every delivered message fail to send,
// so this is the important case).
func TestParseDonateArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want donateAction
	}{
		{"no args shows state", nil, donateAction{kind: donateShow}},
		{"bare donate shows state", []string{"donate"}, donateAction{kind: donateShow}},

		// The wording from the request, plus the spellings people reach for.
		{"active", []string{"active"}, donateAction{kind: donateToggle, on: true}},
		{"activate", []string{"activate"}, donateAction{kind: donateToggle, on: true}},
		{"enable", []string{"enable"}, donateAction{kind: donateToggle, on: true}},
		{"on", []string{"on"}, donateAction{kind: donateToggle, on: true}},
		{"deactive", []string{"deactive"}, donateAction{kind: donateToggle, on: false}},
		{"deactivate", []string{"deactivate"}, donateAction{kind: donateToggle, on: false}},
		{"disable", []string{"disable"}, donateAction{kind: donateToggle, on: false}},
		{"off", []string{"off"}, donateAction{kind: donateToggle, on: false}},

		// Case-insensitive, and reachable through the /admin sub-command form.
		{"uppercase", []string{"ACTIVE"}, donateAction{kind: donateToggle, on: true}},
		{"via admin donate", []string{"donate", "deactive"}, donateAction{kind: donateToggle, on: false}},

		{"set link", []string{"link", "https://x.example/pay"},
			donateAction{kind: donateSetLink, link: "https://x.example/pay"}},
		{"set link via set", []string{"set", "http://x.example"},
			donateAction{kind: donateSetLink, link: "http://x.example"}},
		{"reset link", []string{"link", "reset"}, donateAction{kind: donateResetLink}},
		{"reset is case-insensitive", []string{"link", "RESET"}, donateAction{kind: donateResetLink}},
		{"via admin donate link", []string{"donate", "link", "https://y.example"},
			donateAction{kind: donateSetLink, link: "https://y.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDonateArgs(tc.args)
			if err != nil {
				t.Fatalf("parseDonateArgs(%q) err = %v; want nil", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseDonateArgs(%q) = %+v; want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseDonateArgsRejects(t *testing.T) {
	// A link that is not http(s) must be refused, not stored: Telegram rejects the
	// keyboard outright, which would break sending for everyone.
	for _, bad := range []string{
		"donate.example/pay", // no scheme
		"tg://resolve?domain=x",
		"javascript:alert(1)",
		"ftp://x.example",
		"'; DROP TABLE users;--",
	} {
		if _, err := parseDonateArgs([]string{"link", bad}); !errors.Is(err, errBadDonateURL) {
			t.Errorf("parseDonateArgs(link %q) err = %v; want errBadDonateURL", bad, err)
		}
	}

	// Unknown verbs and a missing url are syntax errors.
	for _, args := range [][]string{
		{"maybe"},
		{"activee"},
		{"link"},           // no url
		{"set"},            // no url
		{"donate", "link"}, // no url via the /admin form
	} {
		if _, err := parseDonateArgs(args); !errors.Is(err, errWrongSyntax) {
			t.Errorf("parseDonateArgs(%q) err = %v; want errWrongSyntax", args, err)
		}
	}
}
