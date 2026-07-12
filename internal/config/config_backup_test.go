package config

import "testing"

// TestAdminIDAllowed locks the BACKUP_ADMIN_ID guard: only a genuine ADMINS
// member is accepted, so a typo (or a group/channel id) can't arm a nightly
// full-DB send to the wrong chat.
func TestAdminIDAllowed(t *testing.T) {
	admins := []string{"1000000001", " 1000000002 ", "1000000003"} // note the padded entry
	cases := []struct {
		id   string
		want bool
	}{
		{"1000000002", true}, // matches the whitespace-padded ADMINS entry
		{"1000000001", true},
		{"1000000003", true},
		{"1000000009", false},     // single-digit typo -> rejected
		{"-1001000000001", false}, // a group/channel id -> rejected
		{"0", false},
		{"", false},
	}
	for _, c := range cases {
		if got := adminIDAllowed(admins, c.id); got != c.want {
			t.Errorf("adminIDAllowed(%q) = %v; want %v", c.id, got, c.want)
		}
	}
	if adminIDAllowed(nil, "1000000002") {
		t.Error("adminIDAllowed(nil, ...) = true; want false (no admins configured)")
	}
}
