package config

import (
	"os"
	"path/filepath"
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
