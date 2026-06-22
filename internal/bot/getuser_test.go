package bot

import "testing"

func TestHrefUser(t *testing.T) {
	// the tg:// deep-link the bot embeds in admin/report/unblock messages.
	if got := hrefUser("123", "u"); got != `<a href="tg://user?id=123">u123</a>` {
		t.Errorf("hrefUser with preText = %q", got)
	}
	// the /start UNBLOCK reply passes an empty preText (start.go).
	if got := hrefUser("123", ""); got != `<a href="tg://user?id=123">123</a>` {
		t.Errorf("hrefUser no preText = %q", got)
	}
}

func TestGetUserLinks(t *testing.T) {
	// two links: 1-based numbering, joined by the "------------\n" separator.
	got := getUserLinks([]string{"a", "b"}, "bot", "")
	want := "<b>لینک 1:</b> t.me/bot?start=a\n------------\n<b>لینک 2:</b> t.me/bot?start=b\n"
	if got != want {
		t.Errorf("getUserLinks two =\n %q\nwant\n %q", got, want)
	}

	// flagCID stars exactly the matching entry with "* ".
	gotFlag := getUserLinks([]string{"a", "b"}, "bot", "b")
	wantFlag := "<b>لینک 1:</b> t.me/bot?start=a\n------------\n<b>* لینک 2:</b> t.me/bot?start=b\n"
	if gotFlag != wantFlag {
		t.Errorf("getUserLinks flagged =\n %q\nwant\n %q", gotFlag, wantFlag)
	}

	// single cid: no separator.
	if got := getUserLinks([]string{"x"}, "bot", ""); got != "<b>لینک 1:</b> t.me/bot?start=x\n" {
		t.Errorf("getUserLinks single = %q", got)
	}

	// empty: empty string.
	if got := getUserLinks(nil, "bot", ""); got != "" {
		t.Errorf("getUserLinks empty = %q; want \"\"", got)
	}
}

func TestUserLinksText(t *testing.T) {
	got := userLinksText([]string{"a"}, 2, "bot")
	want := "<b>لینک 1:</b> t.me/bot?start=a\n\n\n1 از 2 لینک مجاز استفاده شده"
	if got != want {
		t.Errorf("userLinksText =\n %q\nwant\n %q", got, want)
	}
}
