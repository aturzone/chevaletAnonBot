package config

import "testing"

// TestAdminIDAllowed locks the BACKUP_ADMIN_ID guard: only a genuine ADMINS
// member is accepted, so a typo (or a group/channel id) can't arm a nightly
// full-DB send to the wrong chat.
func TestAdminIDAllowed(t *testing.T) {
	admins := []string{"84581926", " 6857450344 ", "6082663084"} // note the padded entry
	cases := []struct {
		id   string
		want bool
	}{
		{"6857450344", true}, // matches the whitespace-padded ADMINS entry
		{"84581926", true},
		{"6082663084", true},
		{"6857450340", false},     // single-digit typo -> rejected
		{"-1001960197379", false}, // a group/channel id -> rejected
		{"0", false},
		{"", false},
	}
	for _, c := range cases {
		if got := adminIDAllowed(admins, c.id); got != c.want {
			t.Errorf("adminIDAllowed(%q) = %v; want %v", c.id, got, c.want)
		}
	}
	if adminIDAllowed(nil, "6857450344") {
		t.Error("adminIDAllowed(nil, ...) = true; want false (no admins configured)")
	}
}
