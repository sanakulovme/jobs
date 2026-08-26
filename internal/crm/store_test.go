package crm

import (
	"sync"
	"testing"
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
