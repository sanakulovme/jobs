package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
	"faangjobs/internal/registry"
	"faangjobs/internal/source"
	"faangjobs/internal/store"
)

// directionScrapeConfig describes how to scrape one direction's vacancies
// via the shared Bundesagentur adapter — same source, a different search
// term and a different on-demand pool per direction, so postings from
// different verticals never mix. Slug feeds registry.Company.EnsureID(),
// which is what makes each direction's pool a single, stable company id
// (e.g. "bundesagentur-mfa-ondemand") that every candidate/city scrape for
// that direction writes into — never a fresh id per candidate/click, so
// model.Job.ID (which is prefixed by the company id, see bundesagentur.go)
// naturally dedupes the same real-world posting across scrape runs. A
// direction with no entry here has no working scraper yet (shown as "tez
// orada" in the UI) — Til kursi and Au pair, until each has a source that
// actually returns relevant results.
//
// Til kursi was tried here with "was": "Sprachkurs" and reverted: verified
// live against Bundesagentur's real API, it returned zero relevant results
// (Mechatroniker, Industrieelektriker, Produktionsmitarbeiter — Bundesagentur
// is a job board, not a course-enrollment platform, and its "was" search is
// loose enough that an unmatched multi-word query just returns noise).
// Ausbildung's own results had the same problem — "Ausbildung Pflege" pulled
// in clearly unrelated postings (Ayurveda-Therapeut, Bäckergeselle,
// BIM-Modeler) alongside the real ones — fixed by titleMustContain below,
// which keeps only postings whose title actually names the word being
// searched for; MFA/ZFA needs no such filter since its search term
// ("Medizinische Fachangestellte") already returns clean results.
type directionScrapeConfig struct {
	searchTerm       string // Bundesagentur "was" query
	slug             string
	titleMustContain string // case-insensitive; "" skips the check
}

var directionScrapeConfigs = map[string]directionScrapeConfig{
	crm.DirectionMFAZFA:     {searchTerm: "Medizinische Fachangestellte", slug: "mfa-ondemand"},
	crm.DirectionAusbildung: {searchTerm: "Ausbildung Pflege", slug: "ausbildung-ondemand", titleMustContain: "ausbildung"},
}

func (a *CRMAPI) registerScrapeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/crm/candidates/{id}/scrape", a.scrapeCandidate)
}

type scrapeInput struct {
	City     string `json:"city"`
	RadiusKm int    `json:"radiusKm"`
	OnlyNew  bool   `json:"onlyNew"`
	Count    int    `json:"count"`
	TestMode bool   `json:"testMode"`
}

// scrapeCandidate is the candidate-centric replacement for the old global
// nightly crawl: fetch fresh MFA/ZFA vacancies scoped to one candidate's
// city, merge them into the shared on-demand pool (deduped by Job.ID), then
// immediately run auto-apply scoped to just this candidate and just the
// jobs this scrape turned up (or only the genuinely new ones, if OnlyNew) —
// reusing runAutoApplyOn, so the same testMode default/gate as /api/crm/run
// applies here too.
func (a *CRMAPI) scrapeCandidate(w http.ResponseWriter, r *http.Request) {
	if a.jobStore == nil || a.fetcher == nil || a.idx == nil {
		writeError(w, http.StatusServiceUnavailable, "scrape sozlanmagan (server jobStore/fetcher bilan ishga tushirilmagan)")
		return
	}
	var in scrapeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.City == "" {
		writeError(w, http.StatusBadRequest, "city is required")
		return
	}

	candidate, err := a.store.GetCandidate(r.PathValue("id"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, ok := directionScrapeConfigs[candidate.Direction]
	if !ok {
		writeError(w, http.StatusNotImplemented, "bu yo'nalish uchun scrape hali ulanmagan")
		return
	}

	radius := in.RadiusKm
	if radius <= 0 {
		radius = 50
	}

	allJobs, newJobs, err := a.scrapeOndemand(r.Context(), cfg, in.City, radius)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scrape muvaffaqiyatsiz: "+err.Error())
		return
	}

	scoped := allJobs
	if in.OnlyNew {
		scoped = newJobs
	}

	result, err := a.runAutoApplyOn(scoped, []crm.Candidate{candidate}, in.Count, in.TestMode)
	if err != nil {
		if errors.Is(err, errGmailNotConfigured) {
			writeError(w, http.StatusNotImplemented, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"foundJobs": len(allJobs),
		"newJobs":   len(newJobs),
		"runId":     result.Run.ID,
		"stats":     result.Stats,
		"drafts":    result.Drafts,
		"dryRun":    in.TestMode,
	}, 0)
}

// scrapeOndemand fetches fresh vacancies for one city from Bundesagentur
// using cfg's search term, merges them into that direction's on-demand
// company pool (deduped by Job.ID — see directionScrapeConfigs), and
// returns both the full set this scrape turned up (all) and just the ones
// that weren't already in the pool before this call (newOnes). Mirrors
// internal/httpapi/index.go's AddManualJob idiom of calling idx.Reload()
// explicitly after a write, rather than waiting on the periodic poll.
func (a *CRMAPI) scrapeOndemand(ctx context.Context, cfg directionScrapeConfig, city string, radiusKm int) (all, newOnes []model.Job, err error) {
	src, ok := source.Get("bundesagentur")
	if !ok {
		return nil, nil, fmt.Errorf("bundesagentur source adapter not registered")
	}
	company := registry.Company{
		Name: "Bundesagentur (on-demand, " + city + ")",
		ATS:  "bundesagentur",
		Slug: cfg.slug,
		Config: map[string]string{
			"was":     cfg.searchTerm,
			"wo":      city,
			"umkreis": strconv.Itoa(radiusKm),
			"maxJobs": "500",
		},
	}
	company.EnsureID() // -> stable per-direction id, deterministically, regardless of city

	fetched, err := src.Fetch(ctx, a.fetcher, company)
	if err != nil {
		return nil, nil, err
	}
	fetched = filterByTitle(fetched, cfg.titleMustContain)

	existing, err := a.jobStore.ReadCompany(company.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	var existingJobs []model.Job
	if existing != nil {
		existingJobs = existing.Jobs
	}
	merged, newOnes := mergeJobsByID(existingJobs, fetched)

	res := store.CompanyResult{
		CompanyID: company.ID,
		Company:   "Bundesagentur fur Arbeit (" + cfg.searchTerm + ", on-demand)",
		ATS:       "bundesagentur",
		Slug:      cfg.slug,
		FetchedAt: time.Now().UTC(),
		OK:        true,
		Jobs:      merged,
		JobCount:  len(merged),
	}
	if err := a.jobStore.WriteCompany(res); err != nil {
		return nil, nil, err
	}
	a.idx.Reload()

	return fetched, newOnes, nil
}

// mergeJobsByID merges fetched into existing, keeping every existing job and
// appending only the fetched ones whose Job.ID isn't already present —
// model.Job.ID is a content hash of the source's own reference number
// (prefixed by the company id, which onDemandCompanyID holds fixed across
// every candidate/city), so the same real-world posting found again simply
// isn't "new" a second time. Returns the merged set and just the newly-added
// ones (in fetched's original order).
func mergeJobsByID(existing, fetched []model.Job) (merged, newOnes []model.Job) {
	seen := make(map[string]bool, len(existing))
	merged = existing
	for _, j := range existing {
		seen[j.ID] = true
	}
	for _, j := range fetched {
		if seen[j.ID] {
			continue
		}
		merged = append(merged, j)
		newOnes = append(newOnes, j)
		seen[j.ID] = true
	}
	return merged, newOnes
}

// filterByTitle keeps only jobs whose Title contains needle
// (case-insensitive); an empty needle is a no-op. This is the fix for
// Bundesagentur's "was" search being a loose keyword match rather than an
// exact one — a multi-word searchTerm like "Ausbildung Pflege" pulls in
// postings that only vaguely relate to the query unless the title itself
// is also checked.
func filterByTitle(jobs []model.Job, needle string) []model.Job {
	if needle == "" {
		return jobs
	}
	needle = strings.ToLower(needle)
	kept := jobs[:0]
	for _, j := range jobs {
		if strings.Contains(strings.ToLower(j.Title), needle) {
			kept = append(kept, j)
		}
	}
	return kept
}
