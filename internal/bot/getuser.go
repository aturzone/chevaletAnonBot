package bot

import (
	"fmt"
	"strconv"
	"strings"
)

// hrefUser mirrors get_user.href_user: an HTML tg:// deep link to a user.
func hrefUser(userid, preText string) string {
	return fmt.Sprintf(`<a href="tg://user?id=%s">%s%s</a>`, userid, preText, userid)
}

// getUserLinks mirrors get_user.get_user_links: a formatted list of a user's
// anonymous /start links. The cid equal to flagCID (if any) is marked with "* ".
func getUserLinks(cids []string, botUsername, flagCID string) string {
	parts := make([]string, 0, len(cids))
	for idx, cid := range cids {
		star := ""
		if flagCID == cid {
			star = "* "
		}
		parts = append(parts, fmt.Sprintf(
			"<b>%sلینک %d:</b> t.me/%s?start=%s\n", star, idx+1, botUsername, cid,
		))
	}
	return strings.Join(parts, "------------\n")
}

// userLinksText mirrors get_user.user_links_text.
func userLinksText(cids []string, cidLimit int, botUsername string) string {
	return fmt.Sprintf(
		"%s\n\n%d از %d لینک مجاز استفاده شده",
		getUserLinks(cids, botUsername, ""), len(cids), cidLimit,
	)
}

// getUsername mirrors get_user.get_username: "@<username>" or "" on API error.
//
// It faithfully reproduces the Python quirk that a user without a username
// yields "@None" (the original f-string interpolated a None username).
func (b *Bot) getUsername(userid string) string {
	id, err := strconv.ParseInt(userid, 10, 64)
	if err != nil {
		return ""
	}
	chat, err := b.TG.GetChat(id, nil)
	if err != nil {
		return ""
	}
	un := chat.Username
	if un == "" {
		un = "None"
	}
	return "@" + un
}

// getLinkUsername mirrors get_user.get_link_username: "<href> | @<username>".
func (b *Bot) getLinkUsername(userid string) string {
	return hrefUser(userid, "u") + " | " + b.getUsername(userid)
}
