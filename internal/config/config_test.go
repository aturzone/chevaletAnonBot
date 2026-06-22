package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHHMM(t *testing.T) {
	cases := []struct {
		raw     string
		want    [2]int
		wantErr bool
	}{
		{"08:30", [2]int{8, 30}, false},
		{"8:5", [2]int{8, 5}, false},
		{" 8 : 5 ", [2]int{8, 5}, false}, // each part is TrimSpace'd
		{"23:59", [2]int{23, 59}, false},
		{"00:00", [2]int{0, 0}, false},
		{"", [2]int{0, 0}, false},     // empty is allowed (early return, no error)
		{"0830", [2]int{0, 0}, true},  // no colon
		{"aa:bb", [2]int{0, 0}, true}, // non-numeric
		{"8:", [2]int{0, 0}, true},    // missing minute
	}
	for _, c := range cases {
		var errs []string
		got := parseHHMM("GM_TIME", c.raw, &errs)
		if got != c.want {
			t.Errorf("parseHHMM(%q) = %v; want %v", c.raw, got, c.want)
		}
		if (len(errs) > 0) != c.wantErr {
			t.Errorf("parseHHMM(%q) errs = %v; wantErr=%v", c.raw, errs, c.wantErr)
		}
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# a comment\n" +
		"\n" +
		"export CHEVALET_TEST_A=hello\n" +
		"CHEVALET_TEST_B=\"quoted value\"\n" +
		"CHEVALET_TEST_C='single'\n" +
		"CHEVALET_TEST_EMPTY=\n" +
		"CHEVALET_TEST_D=should-not-override\n" +
		"malformed line without equals\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// D is already set -> loadDotEnv must NOT override it (python-dotenv default).
	t.Setenv("CHEVALET_TEST_D", "original")
	for _, k := range []string{"CHEVALET_TEST_A", "CHEVALET_TEST_B", "CHEVALET_TEST_C", "CHEVALET_TEST_EMPTY"} {
		key := k
		t.Cleanup(func() { os.Unsetenv(key) })
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	checks := map[string]string{
		"CHEVALET_TEST_A":     "hello",
		"CHEVALET_TEST_B":     "quoted value", // surrounding double quotes stripped
		"CHEVALET_TEST_C":     "single",       // surrounding single quotes stripped
		"CHEVALET_TEST_EMPTY": "",
		"CHEVALET_TEST_D":     "original", // preserved, not overridden
	}
	for k, want := range checks {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q; want %q", k, got, want)
		}
	}
}

func TestLoadDotEnvMissingFileIsOK(t *testing.T) {
	// a missing .env is not an error (python-dotenv default).
	if err := loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Errorf("loadDotEnv(missing) = %v; want nil", err)
	}
}

var requiredEnv = map[string]string{
	"BOT_TOKEN":         "7568852524:ABCdef",
	"DEFAULT_CID_LIMIT": "5",
	"MAX_NAME_LENGTH":   "50",
	"MAX_CID_LENGTH":    "20",
	"MIN_CID_LENGTH":    "3",
	"HEALTH_PORT":       "8080",
	"SEND_GM_GN":        "true",
	"GM_TIME":           "08:00",
	"GN_TIME":           "22:30",
	"GM_GROUP_ID":       "-100123",
	"DONATION_LINK":     "https://donate",
}

func setRequiredEnv(t *testing.T) {
	t.Helper()
	for k, v := range requiredEnv {
		t.Setenv(k, v)
	}
}

func TestConfigLoad(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ADMINS", "111|222")
	t.Setenv("AI_ENABLED", "TRUE") // case-insensitive
	// force these empty so the defaults apply deterministically regardless of the
	// ambient environment the test runs in.
	t.Setenv("DB_PORT", "")
	t.Setenv("AI_INTERVAL", "")
	t.Setenv("BOT_HEALTH_PORT", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"BotToken", c.BotToken, "7568852524:ABCdef"},
		{"BotID (before ':')", c.BotID, "7568852524"},
		{"SendGMGN", c.SendGMGN, true},
		{"GMTime", c.GMTime, [2]int{8, 0}},
		{"GNTime", c.GNTime, [2]int{22, 30}},
		{"AIEnabled (case-insensitive)", c.AIEnabled, true},
		{"DBPort default", c.DBPort, 5432},
		{"AIInterval default", c.AIInterval, 5},
		{"BotHealthPort falls back to HEALTH_PORT", c.BotHealthPort, 8080},
		{"HealthPort", c.HealthPort, 8080},
		{"MaxNameLength", c.MaxNameLength, 50},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v; want %v", ch.name, ch.got, ch.want)
		}
	}
	if len(c.Admins) != 2 || c.Admins[0] != "111" || c.Admins[1] != "222" {
		t.Errorf("Admins = %v; want [111 222] (split on '|')", c.Admins)
	}

	// AI_ENABLED defaults to false when unset.
	t.Setenv("AI_ENABLED", "")
	if c2, err := Load(); err != nil || c2.AIEnabled {
		t.Errorf("AIEnabled with AI_ENABLED unset = %v (err %v); want false", c2.AIEnabled, err)
	}
}

func TestConfigLoadErrors(t *testing.T) {
	// every required var empty -> one aggregated "missing required" error.
	for k := range requiredEnv {
		t.Setenv(k, "")
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "missing required") {
		t.Fatalf("Load() with all required empty = %v; want a 'missing required' error", err)
	}

	// a non-integer length is reported as an integer error, not a panic.
	setRequiredEnv(t)
	t.Setenv("DEFAULT_CID_LIMIT", "notanint")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "DEFAULT_CID_LIMIT must be an integer") {
		t.Fatalf("Load() with bad int = %v; want a 'must be an integer' error", err)
	}
}
