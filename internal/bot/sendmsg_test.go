package bot

import (
	"errors"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// midsFromRows flattens the delete buttons back to their packed message ids
// (everything after "delete|<encChid>" in each button's callback_data).
func midsFromRows(t *testing.T, rows [][]gotgbot.InlineKeyboardButton, encChid string) []string {
	t.Helper()
	var got []string
	for _, r := range rows {
		if len(r) > 2 {
			t.Fatalf("row has %d buttons; want <=2", len(r))
		}
		for _, btn := range r {
			if len(btn.CallbackData) > 64 {
				t.Errorf("button callback_data %q is %d bytes (>64)", btn.CallbackData, len(btn.CallbackData))
			}
			fields := strings.Split(btn.CallbackData, "|")
			if len(fields) < 3 || fields[0] != "delete" || fields[1] != encChid {
				t.Fatalf("malformed delete data %q (want delete|%s|...)", btn.CallbackData, encChid)
			}
			got = append(got, fields[2:]...)
		}
	}
	return got
}

func TestPackDeleteButtons(t *testing.T) {
	// few short mids -> a single button in a single row.
	rows := packDeleteButtons("ABC", []string{"1", "2", "3"})
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("short case shape = %v; want one 1-button row", rows)
	}
	if rows[0][0].CallbackData != "delete|ABC|1|2|3" {
		t.Errorf("short case data = %q; want delete|ABC|1|2|3", rows[0][0].CallbackData)
	}

	// overflow: a long chid so each button holds exactly one mid (1 fits, 2 overflow).
	encChid := strings.Repeat("A", 52) // delete|<52>|XX = 62 bytes; +|XX = 67 > 64
	mids := []string{"10", "20", "30", "40", "50"}
	rows = packDeleteButtons(encChid, mids)
	// 5 single-mid buttons -> rows of 2,2,1.
	if len(rows) != 3 || len(rows[0]) != 2 || len(rows[1]) != 2 || len(rows[2]) != 1 {
		t.Fatalf("overflow row shape = %v; want [2 2 1]", rowSizes(rows))
	}
	got := midsFromRows(t, rows, encChid)
	if strings.Join(got, ",") != strings.Join(mids, ",") {
		t.Errorf("packed mids = %v; want %v (each once, in order)", got, mids)
	}

	// empty mids -> no buttons (the trailing template-only flush is suppressed).
	if rows := packDeleteButtons("ABC", nil); len(rows) != 0 {
		t.Errorf("empty mids = %v; want no rows", rows)
	}
}

func rowSizes(rows [][]gotgbot.InlineKeyboardButton) []int {
	s := make([]int, len(rows))
	for i, r := range rows {
		s[i] = len(r)
	}
	return s
}

// TestSendOutcomeToState locks the del-sentinel -> conversation-transition map:
// outcomeState0 keeps composing (NextState "0"); outcomeEnd ends the conversation.
func TestSendOutcomeToState(t *testing.T) {
	var c0 *handlers.ConversationStateChange
	if !errors.As(outcomeState0.toState(), &c0) {
		t.Fatal("outcomeState0.toState() should be a ConversationStateChange")
	}
	if c0.End || c0.NextState == nil || *c0.NextState != stateSending {
		t.Errorf("outcomeState0 -> %+v; want NextState=%q, End=false", c0, stateSending)
	}

	var cE *handlers.ConversationStateChange
	if !errors.As(outcomeEnd.toState(), &cE) {
		t.Fatal("outcomeEnd.toState() should be a ConversationStateChange")
	}
	if !cE.End || cE.NextState != nil {
		t.Errorf("outcomeEnd -> %+v; want End=true, NextState=nil", cE)
	}
}
