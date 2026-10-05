package crm

import (
	"fmt"
	"sort"
	"strings"

	"faangjobs/internal/model"
)

// germanLevels is ordered weakest to strongest; index doubles as the
// comparison value (same table the reference app's CandidateMatcher uses).
var germanLevels = []string{"A1", "A2", "B1", "B2", "C1", "C2"}

func germanLevelIndex(level string) (int, bool) {
	level = strings.ToUpper(strings.TrimSpace(level))
	for i, l := range germanLevels {
		if l == level {
			return i, true
		}
	}
	return 0, false
}

// Scoring weights, ported verbatim from the reference app's CandidateMatcher.
const (
	scorePerSpecialty    = 40
	scoreGermanOK        = 25
	scoreHasCV           = 15
	scoreGmailReady      = 20
	scorePerExperienceYr = 2
	maxExperienceBonus   = 20
)

// Match is one candidate profile scored against a vacancy, ranked best first.
type Match struct {
	Candidate           Candidate          `json:"candidate"`
	Profile             ApplicationProfile `json:"profile"`
	Score               int                `json:"score"`
	MatchedSpecialties  []string           `json:"matchedSpecialties"`
	GermanOK            bool               `json:"germanOk"`
	GmailReady          bool               `json:"gmailReady"`
	HasCV               bool               `json:"hasCv"`
	Blockers            []string           `json:"blockers"`
	AlreadySent         bool               `json:"alreadySent"`
	ExistingApplication *Application       `json:"existingApplication,omitempty"`
}

// MatchVacancy scores every candidate's best-fitting profile against job and
// returns the matches best-first. A candidate with no application profile at
// all (nothing to attach, nothing to compare) is skipped entirely — matching
// the reference app's "not a match, just an incomplete record" rule.
func MatchVacancy(candidates []Candidate, applications []Application, job model.Job) []Match {
	wanted := job.Specialties
	requiredLevel, hasRequiredLevel := germanLevelIndex(job.RequiredGermanLevel)

	// Always a non-nil slice: the frontend renders this straight from JSON,
	// and a bare `null` (Go's zero value for []Match) is an easy footgun for
	// callers that assume an array.
	out := []Match{}
	for _, c := range candidates {
		best, ok := bestProfile(c, wanted)
		if !ok {
			continue
		}

		candidateLevel, hasCandidateLevel := germanLevelIndex(c.GermanLevel)
		germanOK := !hasRequiredLevel || (hasCandidateLevel && candidateLevel >= requiredLevel)
		gmailReady := c.HasCapacity()

		score := best.score +
			boolScore(germanOK, scoreGermanOK) +
			boolScore(gmailReady, scoreGmailReady) +
			boolScore(best.hasCV, scoreHasCV)

		blockers := blockersFor(gmailReady, best.hasCV, germanOK, best.matched)

		m := Match{
			Candidate:          c,
			Profile:            best.profile,
			Score:              score,
			MatchedSpecialties: best.matched,
			GermanOK:           germanOK,
			GmailReady:         gmailReady,
			HasCV:              best.hasCV,
			Blockers:           blockers,
		}
		if app, ok := existingApplication(applications, c.ID, job.ID); ok {
			m.AlreadySent = true
			appCopy := app
			m.ExistingApplication = &appCopy
		}
		out = append(out, m)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func boolScore(b bool, points int) int {
	if b {
		return points
	}
	return 0
}

// blockersFor lists (in Uzbek, matching the reference app's UI copy) every
// hard reason a match can't be sent regardless of score.
func blockersFor(gmailReady, hasCV, germanOK bool, matched []string) []string {
	out := []string{}
	if !gmailReady {
		out = append(out, "Gmail ulanmagan")
	}
	if !hasCV {
		out = append(out, "CV biriktirilmagan")
	}
	if !germanOK {
		out = append(out, "Nemis tili darajasi yetarli emas")
	}
	if len(matched) == 0 {
		out = append(out, "Yo'nalish mos kelmadi")
	}
	return out
}

type profileScore struct {
	profile ApplicationProfile
	score   int
	matched []string
	hasCV   bool
}

// bestProfile returns the candidate's highest-scoring profile against wanted
// specialties, or ok=false if the candidate has no profiles at all.
func bestProfile(c Candidate, wanted []string) (profileScore, bool) {
	var best profileScore
	found := false
	for _, p := range c.Profiles {
		s := scoreProfile(p, wanted)
		if !found || s.score > best.score {
			best, found = s, true
		}
	}
	return best, found
}

func scoreProfile(p ApplicationProfile, wanted []string) profileScore {
	wantedSet := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		wantedSet[w] = true
	}

	matched := []string{}
	experienceBonus := 0
	for _, ps := range p.Specialties {
		if !wantedSet[ps.Specialty] {
			continue
		}
		matched = append(matched, ps.Specialty)
		experienceBonus += ps.ExperienceYears * scorePerExperienceYr
	}
	if experienceBonus > maxExperienceBonus {
		experienceBonus = maxExperienceBonus
	}

	return profileScore{
		profile: p,
		score:   len(matched)*scorePerSpecialty + experienceBonus,
		matched: matched,
		hasCV:   p.CVDocumentID != "",
	}
}

func existingApplication(applications []Application, candidateID, jobID string) (Application, bool) {
	for _, a := range applications {
		if a.CandidateID == candidateID && a.VacancyID == jobID && a.Status != AppStatusFailed {
			return a, true
		}
	}
	return Application{}, false
}

// --- auto-apply gating ---

// AutoApplyOptions mirrors the reference app's AUTO_APPLY_* env vars.
type AutoApplyOptions struct {
	MaxPerRun       int
	MaxPerCandidate int
	MinScore        int
	MinGermanLevel  string // "" = no absolute floor, only the vacancy's own requirement applies
	DryRun          bool
}

// AutoApplyStats summarizes one run, mirroring AutoApplyService::run()'s
// return shape.
type AutoApplyStats struct {
	Considered int      `json:"considered"`
	Sent       int      `json:"sent"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	NoEmail    int      `json:"noEmail"`
	Details    []string `json:"details"`
}

// rejection returns a human-readable (Uzbek) reason the candidate must not
// be auto-applied for, or "" if they may be.
func rejection(m Match, opts AutoApplyOptions, alreadySentForCandidate int) string {
	if len(m.Blockers) > 0 {
		return strings.Join(m.Blockers, ", ")
	}
	if m.Score < opts.MinScore {
		return "score past"
	}
	if opts.MinGermanLevel != "" {
		floor, ok := germanLevelIndex(opts.MinGermanLevel)
		level, hasLevel := germanLevelIndex(m.Candidate.GermanLevel)
		if ok && (!hasLevel || level < floor) {
			return "nemis tili darajasi belgilangan chegaradan past"
		}
	}
	if alreadySentForCandidate >= opts.MaxPerCandidate {
		return "kandidat limiti"
	}
	return ""
}

// ApplyFunc sends one application for a match and returns the created
// Application (status sent/failed already set) or an error. RunAutoApply
// calls it once per accepted match; the caller supplies the actual
// send implementation (dry-run no-op, or a real internal/gmail send) so this
// file has no dependency on internal/gmail.
type ApplyFunc func(job model.Job, m Match) (Application, error)

// RunAutoApply walks vacancies best-first, matches each against candidates,
// and calls apply for every match that clears every gate: no blockers, score
// at or above opts.MinScore, an absolute German floor (if configured), and
// both the per-run and per-candidate send caps. Mirrors the reference app's
// AutoApplyService::run() exactly, including its early-exit conditions.
func RunAutoApply(vacancies []model.Job, candidates []Candidate, applications []Application, opts AutoApplyOptions, apply ApplyFunc) AutoApplyStats {
	// Details starts as []string{}, not nil: the same "nil slice marshals to
	// JSON null and crashes a frontend .map()" trap documented on Match above.
	stats := AutoApplyStats{Details: []string{}}
	sentPerCandidate := map[string]int{}

	for _, job := range vacancies {
		if stats.Sent >= opts.MaxPerRun {
			stats.Details = append(stats.Details, fmt.Sprintf("Limitga yetildi (%d ta xat).", opts.MaxPerRun))
			break
		}
		if job.ApplicationEmail == "" {
			stats.NoEmail++
			continue
		}

		for _, m := range MatchVacancy(candidates, applications, job) {
			if stats.Sent >= opts.MaxPerRun {
				break
			}
			stats.Considered++

			if m.AlreadySent {
				stats.Skipped++
				continue
			}
			if reason := rejection(m, opts, sentPerCandidate[m.Candidate.ID]); reason != "" {
				stats.Skipped++
				continue
			}

			app, err := apply(job, m)
			if err != nil {
				stats.Failed++
				stats.Details = append(stats.Details, fmt.Sprintf("XATO: %s -> %s: %v", m.Candidate.FullName, job.Company, err))
				continue
			}

			stats.Sent++
			sentPerCandidate[m.Candidate.ID]++
			prefix := ""
			if opts.DryRun {
				prefix = "[DRY] "
			}
			stats.Details = append(stats.Details, fmt.Sprintf("%s%s -> %s (%s)", prefix, m.Candidate.FullName, job.Company, job.ApplicationEmail))
			applications = append(applications, app)
		}
	}

	return stats
}
