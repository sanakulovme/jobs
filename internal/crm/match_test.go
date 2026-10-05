package crm

import (
	"errors"
	"testing"

	"faangjobs/internal/model"
)

func fullCandidate(id string, specialties ...string) Candidate {
	ps := make([]ProfileSpecialty, len(specialties))
	for i, s := range specialties {
		ps[i] = ProfileSpecialty{Specialty: s}
	}
	return Candidate{
		ID:          id,
		FullName:    "Test " + id,
		GermanLevel: "B2",
		GmailMailboxes: []GmailMailbox{
			{Slot: "1", Email: id + "@gmail.com", RefreshToken: "refresh-" + id, DailyCap: 50},
		},
		Profiles: []ApplicationProfile{
			{ID: "p-" + id, CandidateID: id, CVDocumentID: "cv-" + id, Specialties: ps},
		},
	}
}

func TestMatchVacancyScoring(t *testing.T) {
	job := model.Job{ID: "job1", Company: "Praxis X", Specialties: []string{"mfa", "kardiologie"}}
	c := fullCandidate("c1", "mfa", "kardiologie")

	matches := MatchVacancy([]Candidate{c}, nil, job)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	// 2 specialties * 40 + german ok 25 + gmail 20 + cv 15 = 140
	want := 2*scorePerSpecialty + scoreGermanOK + scoreGmailReady + scoreHasCV
	if m.Score != want {
		t.Errorf("score = %d, want %d", m.Score, want)
	}
	if len(m.Blockers) != 0 {
		t.Errorf("expected no blockers, got %v", m.Blockers)
	}
}

func TestMatchVacancyBlockers(t *testing.T) {
	job := model.Job{ID: "job1", Specialties: []string{"mfa"}, RequiredGermanLevel: "C1"}
	c := Candidate{
		ID:       "c2",
		FullName: "No Gmail",
		Profiles: []ApplicationProfile{{ID: "p1", Specialties: nil}}, // no matched specialty, no CV
		// no GmailEmail/RefreshToken -> not connected
		GermanLevel: "A2", // below required C1
	}

	matches := MatchVacancy([]Candidate{c}, nil, job)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	blockers := matches[0].Blockers
	wantBlockers := []string{"Gmail ulanmagan", "CV biriktirilmagan", "Nemis tili darajasi yetarli emas", "Yo'nalish mos kelmadi"}
	if len(blockers) != len(wantBlockers) {
		t.Fatalf("blockers = %v, want %v", blockers, wantBlockers)
	}
	for i, b := range wantBlockers {
		if blockers[i] != b {
			t.Errorf("blocker[%d] = %q, want %q", i, blockers[i], b)
		}
	}
}

func TestMatchVacancySkipsCandidateWithNoProfile(t *testing.T) {
	job := model.Job{ID: "job1", Specialties: []string{"mfa"}}
	c := Candidate{ID: "c3", FullName: "Empty"} // no Profiles at all

	matches := MatchVacancy([]Candidate{c}, nil, job)
	if len(matches) != 0 {
		t.Fatalf("expected candidate with no profiles to be skipped, got %d matches", len(matches))
	}
}

func TestMatchVacancyAlreadySentGuard(t *testing.T) {
	job := model.Job{ID: "job1", Specialties: []string{"mfa"}}
	c := fullCandidate("c4", "mfa")
	apps := []Application{{ID: "a1", CandidateID: "c4", VacancyID: "job1", Status: AppStatusSent}}

	matches := MatchVacancy([]Candidate{c}, apps, job)
	if len(matches) != 1 || !matches[0].AlreadySent {
		t.Fatalf("expected AlreadySent=true, got %+v", matches)
	}
}

func TestMatchVacancyRanksBestFirst(t *testing.T) {
	job := model.Job{ID: "job1", Specialties: []string{"mfa", "kardiologie"}}
	weak := fullCandidate("weak", "mfa")
	strong := fullCandidate("strong", "mfa", "kardiologie")

	matches := MatchVacancy([]Candidate{weak, strong}, nil, job)
	if len(matches) != 2 || matches[0].Candidate.ID != "strong" {
		t.Fatalf("expected strong candidate ranked first, got %+v", matches)
	}
}

func TestRunAutoApplyRespectsMinScoreAndCaps(t *testing.T) {
	job := model.Job{ID: "job1", Company: "Praxis X", ApplicationEmail: "x@praxis.de", Specialties: []string{"mfa", "kardiologie"}}
	// With every soft bonus present (german ok + gmail ready + cv = 60), an
	// unblocked match always scores >= 40+60 = 100 for a single specialty, so
	// a threshold has to sit above that to distinguish "one match" from "two
	// matches" (140) rather than from "blocked".
	weak := fullCandidate("weak", "mfa")                    // 1 specialty -> 100
	strong := fullCandidate("strong", "mfa", "kardiologie") // 2 specialties -> 140

	var applied []string
	stats := RunAutoApply([]model.Job{job}, []Candidate{weak, strong}, nil,
		AutoApplyOptions{MaxPerRun: 20, MaxPerCandidate: 5, MinScore: 120, DryRun: true},
		func(job model.Job, m Match) (Application, error) {
			applied = append(applied, m.Candidate.ID)
			return Application{ID: "app-" + m.Candidate.ID, CandidateID: m.Candidate.ID, VacancyID: job.ID, Status: AppStatusSent}, nil
		})

	if stats.Sent != 1 || len(applied) != 1 || applied[0] != "strong" {
		t.Fatalf("expected only 'strong' to be auto-applied, got stats=%+v applied=%v", stats, applied)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected 1 skipped (weak, below min score), got %d", stats.Skipped)
	}
}

func TestRunAutoApplyStopsAtMaxPerRun(t *testing.T) {
	job1 := model.Job{ID: "j1", ApplicationEmail: "a@x.de", Specialties: []string{"mfa"}}
	job2 := model.Job{ID: "j2", ApplicationEmail: "b@x.de", Specialties: []string{"mfa"}}
	c := fullCandidate("c1", "mfa")

	sentCount := 0
	stats := RunAutoApply([]model.Job{job1, job2}, []Candidate{c}, nil,
		AutoApplyOptions{MaxPerRun: 0, MaxPerCandidate: 5, MinScore: 0},
		func(job model.Job, m Match) (Application, error) {
			sentCount++
			return Application{}, nil
		})

	if sentCount != 0 || stats.Sent != 0 {
		t.Fatalf("expected MaxPerRun=0 to send nothing, got sentCount=%d stats=%+v", sentCount, stats)
	}
}

// TestNoNilSlicesInJSONResponses guards against a bug class that already hit
// production once: a nil Go slice marshals to JSON `null`, and frontend code
// doing `.map()`/`.length` on a field it expects to be an array crashes hard.
// Every slice field returned straight from these functions must be
// non-nil even in the "found nothing" case.
func TestNoNilSlicesInJSONResponses(t *testing.T) {
	stats := RunAutoApply(nil, nil, nil, AutoApplyOptions{},
		func(model.Job, Match) (Application, error) { return Application{}, nil })
	if stats.Details == nil {
		t.Error("AutoApplyStats.Details is nil, wants []string{}")
	}

	// A candidate with no matched specialties and no blockers-tripping gaps
	// still exercises the empty-but-non-nil path in blockersFor/scoreProfile.
	c := Candidate{ID: "c1", Profiles: []ApplicationProfile{{ID: "p1"}}}
	matches := MatchVacancy([]Candidate{c}, nil, model.Job{ID: "j1", Specialties: []string{"mfa"}})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].MatchedSpecialties == nil {
		t.Error("Match.MatchedSpecialties is nil, wants []string{}")
	}
	if matches[0].Blockers == nil {
		t.Error("Match.Blockers is nil, wants []string{}")
	}
}

func TestRunAutoApplyPropagatesApplyError(t *testing.T) {
	job := model.Job{ID: "j1", ApplicationEmail: "a@x.de", Specialties: []string{"mfa"}}
	c := fullCandidate("c1", "mfa")

	stats := RunAutoApply([]model.Job{job}, []Candidate{c}, nil,
		AutoApplyOptions{MaxPerRun: 5, MaxPerCandidate: 5, MinScore: 0},
		func(job model.Job, m Match) (Application, error) {
			return Application{}, errors.New("gmail send failed")
		})

	if stats.Failed != 1 || stats.Sent != 0 {
		t.Fatalf("expected 1 failed, 0 sent, got %+v", stats)
	}
}
