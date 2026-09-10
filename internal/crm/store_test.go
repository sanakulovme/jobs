package crm

import (
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCandidateCRUDRoundTrip(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateCandidate(Candidate{FullName: "Abbos Sanakulov", GermanLevel: "B2"})
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected a non-empty id")
	}

	got, err := s.GetCandidate(created.ID)
	if err != nil {
		t.Fatalf("GetCandidate: %v", err)
	}
	if got.FullName != "Abbos Sanakulov" {
		t.Errorf("FullName = %q", got.FullName)
	}

	updated, err := s.UpdateCandidate(created.ID, func(c Candidate) (Candidate, error) {
		c.Phone = "+998770801563"
		return c, nil
	})
	if err != nil {
		t.Fatalf("UpdateCandidate: %v", err)
	}
	if updated.Phone != "+998770801563" {
		t.Errorf("Phone = %q", updated.Phone)
	}

	if err := s.DeleteCandidate(created.ID); err != nil {
		t.Fatalf("DeleteCandidate: %v", err)
	}
	if _, err := s.GetCandidate(created.ID); err != ErrNotFound() {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDocumentAndProfileNesting(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateCandidate(Candidate{FullName: "Test"})
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}

	doc, err := s.AddDocument(c.ID, Document{Type: "cv", OriginalFilename: "cv.pdf", IsPrimaryCV: true})
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}

	profile, err := s.AddProfile(c.ID, ApplicationProfile{
		Name: "Abbos cv", CVDocumentID: doc.ID,
		Specialties: []ProfileSpecialty{{Specialty: "mfa"}},
	})
	if err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	got, err := s.GetCandidate(c.ID)
	if err != nil {
		t.Fatalf("GetCandidate: %v", err)
	}
	if len(got.Documents) != 1 || got.Documents[0].ID != doc.ID {
		t.Errorf("Documents = %+v", got.Documents)
	}
	if len(got.Profiles) != 1 || got.Profiles[0].ID != profile.ID || got.Profiles[0].CVDocumentID != doc.ID {
		t.Errorf("Profiles = %+v", got.Profiles)
	}

	if err := s.DeleteDocument(c.ID, doc.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	got, _ = s.GetCandidate(c.ID)
	if len(got.Documents) != 0 {
		t.Errorf("expected document removed, got %+v", got.Documents)
	}
}

func TestApplicationDuplicateGuard(t *testing.T) {
	s := newTestStore(t)
	a := Application{CandidateID: "c1", VacancyID: "v1", Status: AppStatusSent}

	if _, err := s.CreateApplication(a); err != nil {
		t.Fatalf("first CreateApplication: %v", err)
	}
	if _, err := s.CreateApplication(a); err != ErrDuplicateApplication() {
		t.Fatalf("expected ErrDuplicateApplication on second create, got %v", err)
	}

	// A failed application for the same pair must not block a retry.
	failed := Application{CandidateID: "c2", VacancyID: "v1", Status: AppStatusFailed}
	if _, err := s.CreateApplication(failed); err != nil {
		t.Fatalf("CreateApplication (different candidate): %v", err)
	}
}

func TestReplyDedup(t *testing.T) {
	s := newTestStore(t)
	r := Reply{ApplicationID: "a1", GmailMessageID: "m1", Category: ReplyGreen}

	_, added, err := s.AddReply(r)
	if err != nil || !added {
		t.Fatalf("first AddReply: added=%v err=%v", added, err)
	}
	_, added, err = s.AddReply(r)
	if err != nil || added {
		t.Fatalf("duplicate AddReply should not add again: added=%v err=%v", added, err)
	}

	all, err := s.ListReplies()
	if err != nil || len(all) != 1 {
		t.Fatalf("expected exactly 1 stored reply, got %d (err=%v)", len(all), err)
	}
}

// TestConcurrentCandidateWrites exercises the table mutex: many goroutines
// creating candidates concurrently must all be persisted, none silently lost
// to a lost-update race.
func TestConcurrentCandidateWrites(t *testing.T) {
	s := newTestStore(t)
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.CreateCandidate(Candidate{FullName: "c"}); err != nil {
				t.Errorf("CreateCandidate: %v", err)
			}
		}(i)
	}
	wg.Wait()

	all, err := s.ListCandidates()
	if err != nil {
		t.Fatalf("ListCandidates: %v", err)
	}
	if len(all) != n {
		t.Fatalf("expected %d candidates, got %d", n, len(all))
	}
	seen := map[string]bool{}
	for _, c := range all {
		if seen[c.ID] {
			t.Fatalf("duplicate id %q", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestMailboxSetClearAndCap(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateCandidate(Candidate{FullName: "Test", Direction: DirectionMFAZFA})
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}

	got, err := s.SetCandidateMailbox(c.ID, "1", "a@gmail.com", "at-1", "rt-1", "scope", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("SetCandidateMailbox: %v", err)
	}
	if len(got.GmailMailboxes) != 1 || got.GmailMailboxes[0].Email != "a@gmail.com" {
		t.Fatalf("GmailMailboxes = %+v", got.GmailMailboxes)
	}

	// Reconnecting the same slot overwrites in place rather than appending.
	got, err = s.SetCandidateMailbox(c.ID, "1", "a2@gmail.com", "at-2", "rt-2", "scope", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("SetCandidateMailbox (reconnect): %v", err)
	}
	if len(got.GmailMailboxes) != 1 || got.GmailMailboxes[0].Email != "a2@gmail.com" {
		t.Fatalf("expected slot 1 overwritten in place, got %+v", got.GmailMailboxes)
	}

	// A 5th distinct slot must be rejected once 4 are connected.
	for _, slot := range []string{"2", "3", "4"} {
		if _, err := s.SetCandidateMailbox(c.ID, slot, slot+"@gmail.com", "at", "rt", "scope", time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("SetCandidateMailbox slot %s: %v", slot, err)
		}
	}
	if _, err := s.SetCandidateMailbox(c.ID, "5", "e@gmail.com", "at", "rt", "scope", time.Now().Add(time.Hour)); err != errTooManyMailboxes {
		t.Errorf("expected errTooManyMailboxes for a 5th slot, got %v", err)
	}

	if _, err := s.SetMailboxDailyCap(c.ID, "1", 999); err != nil {
		t.Fatalf("SetMailboxDailyCap: %v", err)
	}
	got, _ = s.GetCandidate(c.ID)
	mb, ok := got.Mailbox("1")
	if !ok || mb.DailyCap != MaxDailyCapPerMailbox {
		t.Errorf("DailyCap = %d, want clamped to %d", mb.DailyCap, MaxDailyCapPerMailbox)
	}

	got, err = s.ClearCandidateMailbox(c.ID, "1")
	if err != nil {
		t.Fatalf("ClearCandidateMailbox: %v", err)
	}
	if len(got.GmailMailboxes) != 3 {
		t.Fatalf("expected 3 mailboxes left after clearing slot 1, got %d", len(got.GmailMailboxes))
	}
	if _, ok := got.Mailbox("1"); ok {
		t.Error("slot 1 should be gone after ClearCandidateMailbox")
	}
}

func TestIncrementMailboxSentRollsOverByDate(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateCandidate(Candidate{FullName: "Test", Direction: DirectionMFAZFA})
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if _, err := s.SetCandidateMailbox(c.ID, "1", "a@gmail.com", "at", "rt", "scope", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetCandidateMailbox: %v", err)
	}

	if err := s.IncrementMailboxSent(c.ID, "1"); err != nil {
		t.Fatalf("IncrementMailboxSent: %v", err)
	}
	got, _ := s.GetCandidate(c.ID)
	mb, _ := got.Mailbox("1")
	if mb.SentToday != 1 {
		t.Fatalf("SentToday = %d, want 1", mb.SentToday)
	}

	// Simulate a stale counter from yesterday: the next increment must reset
	// to 1, not accumulate on top of a prior day's count.
	if _, err := s.UpdateCandidate(c.ID, func(cand Candidate) (Candidate, error) {
		cand.GmailMailboxes[0].SentTodayDate = "2000-01-01"
		cand.GmailMailboxes[0].SentToday = 40
		return cand, nil
	}); err != nil {
		t.Fatalf("UpdateCandidate: %v", err)
	}
	if err := s.IncrementMailboxSent(c.ID, "1"); err != nil {
		t.Fatalf("IncrementMailboxSent (rollover): %v", err)
	}
	got, _ = s.GetCandidate(c.ID)
	mb, _ = got.Mailbox("1")
	if mb.SentToday != 1 {
		t.Errorf("SentToday after rollover = %d, want 1 (not accumulated on the stale count)", mb.SentToday)
	}
}

func TestVacancyOverride(t *testing.T) {
	s := newTestStore(t)
	if _, ok, err := s.VacancyOverride("job1"); err != nil || ok {
		t.Fatalf("expected no override initially, got ok=%v err=%v", ok, err)
	}
	if err := s.SetVacancyOverride("job1", []string{"mfa", "dialyse"}); err != nil {
		t.Fatalf("SetVacancyOverride: %v", err)
	}
	got, ok, err := s.VacancyOverride("job1")
	if err != nil || !ok || len(got) != 2 {
		t.Fatalf("VacancyOverride = %v, ok=%v, err=%v", got, ok, err)
	}
}
