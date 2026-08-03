package bot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPyBool locks the Python str(bool) casing the /admin stats reply mirrors.
func TestPyBool(t *testing.T) {
	if got := pyBool(true); got != "True" {
		t.Errorf("pyBool(true) = %q; want True", got)
	}
	if got := pyBool(false); got != "False" {
		t.Errorf("pyBool(false) = %q; want False", got)
	}
}

// TestAt locks the positional-arg accessor that maps every missing /admin arg to
// errWrongSyntax (Python IndexError -> "wrong syntax").
func TestAt(t *testing.T) {
	args := []string{"a", "b"}
	if v, err := at(args, 0); err != nil || v != "a" {
		t.Errorf("at(args,0) = (%q,%v); want (a,nil)", v, err)
	}
	if v, err := at(args, 1); err != nil || v != "b" {
		t.Errorf("at(args,1) = (%q,%v); want (b,nil)", v, err)
	}
	for _, i := range []int{2, -1, 99} {
		if _, err := at(args, i); !errors.Is(err, errWrongSyntax) {
			t.Errorf("at(args,%d) err = %v; want errWrongSyntax", i, err)
		}
	}
	if _, err := at(nil, 0); !errors.Is(err, errWrongSyntax) {
		t.Errorf("at(nil,0) err = %v; want errWrongSyntax", err)
	}
}

// TestAdminHelpDocumentsEverySubcommand guards against the drift that makes admin
// help useless: a subcommand added to the dispatcher but never written down. It
// reads the real Texts/admin.txt and asserts every top-level /admin verb the
// dispatcher handles appears in it.
//
// The verb list is maintained by hand next to adminDispatch's switch. That is the
// point — adding a case without adding it here fails this test, which is the
// reminder to document it.
func TestAdminHelpDocumentsEverySubcommand(t *testing.T) {
	verbs := []string{
		"help", "send-mass-msg", "send-msg", "user-count", "stats",
		"ban", "unban", "cid", "link", "report",
		"ai-url", "ai-session", "donate", "backup",
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "Texts", "admin.txt"))
	if err != nil {
		t.Fatalf("reading Texts/admin.txt: %v", err)
	}
	help := string(raw)

	for _, v := range verbs {
		// Either the /admin form or the standalone alias must be documented.
		if !strings.Contains(help, "/admin "+v) && !strings.Contains(help, "/admin_"+v) {
			t.Errorf("admin subcommand %q is handled but not documented in Texts/admin.txt", v)
		}
	}

	// The standalone admin commands registered in registerHandlers.
	for _, cmd := range []string{"/admin_donate", "/admin_reports", "/admin_stats"} {
		if !strings.Contains(help, cmd) {
			t.Errorf("%s is registered but not documented in Texts/admin.txt", cmd)
		}
	}

	// And the panel that now fronts all of it.
	if !strings.Contains(help, "/menu") {
		t.Error("admin help does not mention /menu, which is where the admin panel lives")
	}
}
