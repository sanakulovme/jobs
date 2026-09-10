package crm

import (
	"testing"
	"time"
)

func TestCandidateHasCapacityAndNextMailbox(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")

	c := Candidate{GmailMailboxes: []GmailMailbox{
		{Slot: "1", Email: "a@gmail.com", RefreshToken: "rt", ConnectedAt: time.Unix(1, 0), DailyCap: 1, SentToday: 1, SentTodayDate: today},
		{Slot: "2", Email: "b@gmail.com", RefreshToken: "rt", ConnectedAt: time.Unix(2, 0), DailyCap: 5, SentToday: 3, SentTodayDate: today},
	}}
	if !c.HasCapacity() {
		t.Fatal("expected capacity: slot 2 still has room")
	}
	next, ok := c.NextMailbox()
	if !ok || next.Slot != "2" {
		t.Fatalf("NextMailbox = %+v, ok=%v, want slot 2 (the only one with remaining capacity)", next, ok)
	}

	// Exhaust slot 2 too.
	c.GmailMailboxes[1].SentToday = 5
	if c.HasCapacity() {
		t.Error("expected no capacity once both slots are exhausted")
	}
	if _, ok := c.NextMailbox(); ok {
		t.Error("NextMailbox should report false once no slot has room")
	}
}

func TestCandidateNextMailboxPicksOldestConnected(t *testing.T) {
	c := Candidate{GmailMailboxes: []GmailMailbox{
		{Slot: "1", Email: "a@gmail.com", RefreshToken: "rt", ConnectedAt: time.Unix(100, 0), DailyCap: 10},
		{Slot: "2", Email: "b@gmail.com", RefreshToken: "rt", ConnectedAt: time.Unix(50, 0), DailyCap: 10},
	}}
	next, ok := c.NextMailbox()
	if !ok || next.Slot != "2" {
		t.Fatalf("NextMailbox = %+v, want the earlier-connected slot 2", next)
	}
}

func TestCandidateNextMailboxSkipsUnconnectedAndStaleCounterResets(t *testing.T) {
	c := Candidate{GmailMailboxes: []GmailMailbox{
		{Slot: "1", Email: "", RefreshToken: ""}, // not actually connected
		{Slot: "2", Email: "b@gmail.com", RefreshToken: "rt", DailyCap: 1, SentToday: 1, SentTodayDate: "2000-01-01"}, // stale date -> reads as 0 used
	}}
	next, ok := c.NextMailbox()
	if !ok || next.Slot != "2" {
		t.Fatalf("NextMailbox = %+v, ok=%v, want slot 2 (slot 1 unconnected, slot 2's stale counter reads as unused)", next, ok)
	}
}

func TestGmailMailboxRedactedStripsTokensKeepsCounters(t *testing.T) {
	c := Candidate{GmailMailboxes: []GmailMailbox{
		{Slot: "1", Email: "a@gmail.com", AccessToken: "at", RefreshToken: "rt", Scope: "s", DailyCap: 30, SentToday: 5},
	}}
	r := c.Redacted()
	if len(r.GmailMailboxes) != 1 {
		t.Fatalf("expected 1 mailbox to survive Redacted(), got %d", len(r.GmailMailboxes))
	}
	mb := r.GmailMailboxes[0]
	if mb.AccessToken != "" || mb.RefreshToken != "" || mb.Scope != "" {
		t.Errorf("token fields not stripped: %+v", mb)
	}
	if mb.Email != "a@gmail.com" || mb.DailyCap != 30 || mb.SentToday != 5 {
		t.Errorf("non-sensitive fields should survive Redacted(): %+v", mb)
	}
}

func TestIsDirection(t *testing.T) {
	for _, d := range Directions {
		if !IsDirection(d) {
			t.Errorf("IsDirection(%q) = false, want true", d)
		}
	}
	if IsDirection("not_a_real_direction") {
		t.Error("IsDirection should reject an unknown value")
	}
}
