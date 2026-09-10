package httpapi

import (
	"testing"

	"faangjobs/internal/model"
	"faangjobs/internal/registry"
)

// TestDirectionScrapeConfigsProduceDistinctStableIDs guards the invariant
// scrapeOndemand relies on: every direction's on-demand pool must resolve
// to the same company id regardless of which city triggered the scrape
// (so repeated scrapes merge into one pool), and different directions must
// never collide into the same pool (so an Ausbildung scrape can't merge
// into MFA/ZFA's jobs, or vice versa).
func TestDirectionScrapeConfigsProduceDistinctStableIDs(t *testing.T) {
	ids := map[string]string{}
	for direction, cfg := range directionScrapeConfigs {
		for _, city := range []string{"Berlin", "Rostock"} {
			company := registry.Company{ATS: "bundesagentur", Slug: cfg.slug}
			company.EnsureID()
			if want, ok := ids[direction]; ok {
				if company.ID != want {
					t.Errorf("direction %q: id changed with city %q: got %q, want %q (must be city-independent)", direction, city, company.ID, want)
				}
			} else {
				ids[direction] = company.ID
			}
		}
	}
	seen := map[string]string{}
	for direction, id := range ids {
		if other, ok := seen[id]; ok {
			t.Errorf("directions %q and %q collide on the same company id %q", direction, other, id)
		}
		seen[id] = direction
	}
}

func TestMergeJobsByIDDedupesAndReportsOnlyNew(t *testing.T) {
	existing := []model.Job{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	fetched := []model.Job{{ID: "b", Title: "B (re-scraped)"}, {ID: "c", Title: "C"}}

	merged, newOnes := mergeJobsByID(existing, fetched)

	if len(merged) != 3 {
		t.Fatalf("merged = %d jobs, want 3 (a, b, c with no duplicates)", len(merged))
	}
	ids := map[string]bool{}
	for _, j := range merged {
		if ids[j.ID] {
			t.Fatalf("duplicate id %q in merged result: %+v", j.ID, merged)
		}
		ids[j.ID] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !ids[want] {
			t.Errorf("merged is missing id %q", want)
		}
	}

	if len(newOnes) != 1 || newOnes[0].ID != "c" {
		t.Fatalf("newOnes = %+v, want exactly [{ID: c}] (b was already present)", newOnes)
	}

	// The existing copy of "b" must be kept, not overwritten by the
	// re-scraped one — merge is additive-only, never mutates known jobs.
	for _, j := range merged {
		if j.ID == "b" && j.Title != "B" {
			t.Errorf("existing job %q was overwritten: got title %q, want the original %q", j.ID, j.Title, "B")
		}
	}
}

func TestMergeJobsByIDEmptyExisting(t *testing.T) {
	fetched := []model.Job{{ID: "x"}, {ID: "y"}}
	merged, newOnes := mergeJobsByID(nil, fetched)
	if len(merged) != 2 || len(newOnes) != 2 {
		t.Fatalf("merged=%d newOnes=%d, want 2 and 2 when starting from an empty pool", len(merged), len(newOnes))
	}
}

func TestMergeJobsByIDNoNewJobs(t *testing.T) {
	existing := []model.Job{{ID: "a"}, {ID: "b"}}
	merged, newOnes := mergeJobsByID(existing, existing)
	if len(merged) != 2 {
		t.Fatalf("merged = %d, want 2 (nothing genuinely new)", len(merged))
	}
	if len(newOnes) != 0 {
		t.Fatalf("newOnes = %+v, want none", newOnes)
	}
}

// TestFilterByTitle guards the fix for Bundesagentur's loose "was" search:
// verified live, "Ausbildung Pflege" pulled in postings with no real
// connection to the query (Ayurveda-Therapeut, Bäckergeselle, BIM-Modeler)
// alongside genuine "Ausbildung ..." titles — filterByTitle is what keeps
// only the latter.
func TestFilterByTitle(t *testing.T) {
	jobs := []model.Job{
		{ID: "1", Title: "Ausbildung Anlagenmechaniker (m/w/d)"},
		{ID: "2", Title: "Ayurveda-Therapeut (m/w/d)"},
		{ID: "3", Title: "AUSBILDUNG 2027 - Mechatroniker"}, // case-insensitive match
		{ID: "4", Title: "Bäckergeselle oder -meister (m/w/d)"},
	}

	kept := filterByTitle(jobs, "ausbildung")
	if len(kept) != 2 {
		t.Fatalf("kept = %d jobs, want 2 (only titles actually containing 'ausbildung'): %+v", len(kept), kept)
	}
	for _, j := range kept {
		if j.ID != "1" && j.ID != "3" {
			t.Errorf("unexpected job kept: %+v", j)
		}
	}

	if got := filterByTitle(jobs, ""); len(got) != len(jobs) {
		t.Errorf("empty needle should be a no-op, got %d jobs, want %d", len(got), len(jobs))
	}
}
