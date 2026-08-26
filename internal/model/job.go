// Package model defines the normalized data types shared by the crawler and
// the server. Every ATS adapter maps its raw payload into these types so the
// rest of the system deals with a single, stable schema.
package model

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"
)

// Job is the normalized representation of a single job posting.
//
// Field names in JSON are camelCase because they are consumed directly by the
// React frontend.
type Job struct {
	ID          string    `json:"id"`                    // stable, unique across the whole board
	CompanyID   string    `json:"companyId"`             // registry company id (e.g. "greenhouse-stripe")
	Company     string    `json:"company"`               // human-readable company name
	Title       string    `json:"title"`                 //
	Location    string    `json:"location"`              // primary/display location
	Locations   []string  `json:"locations,omitempty"`   // all locations if the source lists several
	Remote      bool      `json:"remote"`                //
	Relocation  bool      `json:"relocation"`            // source mentions relocation support
	Department  string    `json:"department,omitempty"`  // raw department/team from the source
	URL         string    `json:"url"`                   // canonical apply/detail URL
	Description string    `json:"description,omitempty"` // HTML or text; omitted from list responses
	PostedAt    time.Time `json:"postedAt"`              // first published time (best effort)
	UpdatedAt   time.Time `json:"updatedAt"`             // last updated time (best effort)
	Source      string    `json:"source"`                // ATS/source kind (greenhouse, lever, ...)
	Categories  []string  `json:"categories,omitempty"`  // normalized categories (see Categorize)
	Specialties []string  `json:"specialties,omitempty"` // machine-matchable specialty slugs (see crm.Specialties)

	// The fields below are populated by sources that extract structured facts
	// out of a free-text posting (currently the "bundesagentur" adapter, via
	// best-effort regex heuristics over German text). They are empty when the
	// source posting doesn't mention them or the source doesn't support them.
	ReferenceNumber           string   `json:"referenceNumber,omitempty"`
	MedicalSpecialty          string   `json:"medicalSpecialty,omitempty"`
	ApplicationEmail          string   `json:"applicationEmail,omitempty"`
	ContactPerson             string   `json:"contactPerson,omitempty"`
	Salutation                string   `json:"salutation,omitempty"`
	RequiredGermanLevel       string   `json:"requiredGermanLevel,omitempty"`
	RequiresDriversLicense    bool     `json:"requiresDriversLicense"`
	RequiresOwnCar            bool     `json:"requiresOwnCar"`
	RequiresGermanMFATraining bool     `json:"requiresGermanMfaTraining"`
	RequiredQualifications    []string `json:"requiredQualifications,omitempty"`
	Website                   string   `json:"website,omitempty"`
	ApplicationPortal         string   `json:"applicationPortal,omitempty"`
	MainDuties                string   `json:"mainDuties,omitempty"`
	MandatoryRequirements     string   `json:"mandatoryRequirements,omitempty"`
	PreferredRequirements     string   `json:"preferredRequirements,omitempty"`
}

// SpecialtyVocabulary is the fixed set of machine-matchable specialty slugs a
// Job.Specialties entry may take. It is the shared vocabulary candidate
// application profiles are tagged with too (see internal/crm), so a job and a
// candidate can be matched by simple slug intersection.
var SpecialtyVocabulary = []string{
	"ausbildung", "daf_daz", "dialyse", "kardiologie", "mfa", "mrt",
	"nephrologie", "ophthalmologie", "orthopadie", "pflege", "rontgen", "zfa",
}

// Slim returns a copy of the job without the (potentially large) free-text
// fields, suitable for list/search responses; the full job (via
// GET /api/jobs/{id}) still carries them.
func (j Job) Slim() Job {
	j.Description = ""
	j.MainDuties = ""
	j.MandatoryRequirements = ""
	j.PreferredRequirements = ""
	return j
}

// StableID produces a short, deterministic id from the given parts. It is used
// to build job ids that stay identical across crawls as long as the source's
// own identifiers are stable.
func StableID(parts ...string) string {
	h := fnv.New64a()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(p))
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// remoteHints are substrings that strongly indicate a remote role.
var remoteHints = []string{"remote", "anywhere", "work from home", "wfh", "distributed"}

// DetectRemote reports whether any of the supplied strings indicate a remote role.
func DetectRemote(fields ...string) bool {
	for _, f := range fields {
		lf := strings.ToLower(f)
		for _, h := range remoteHints {
			if strings.Contains(lf, h) {
				return true
			}
		}
	}
	return false
}

// category keyword tables. Order matters: the first matching category wins for
// the "primary" slot, but a job may accumulate several categories.
var categoryKeywords = []struct {
	name     string
	keywords []string
}{
	{"Software Engineering", []string{"software engineer", "swe", "developer", "programmer", "full stack", "fullstack", "full-stack", "backend", "back end", "back-end", "frontend", "front end", "front-end", "mobile engineer", "ios", "android", "web engineer", "platform engineer", "systems engineer"}},
	{"Data & ML", []string{"machine learning", "ml engineer", "ai engineer", "ai/ml", "genai", "generative ai", "data scien", "data engineer", "deep learning", "ai research", "applied scien", "research scien", "nlp", "computer vision", "analytics engineer", "mlops", "llm"}},
	{"Infrastructure & DevOps", []string{"devops", "site reliability", "sre", "infrastructure", "platform reliability", "cloud engineer", "network engineer", "systems administrator", "kubernetes"}},
	{"Security", []string{"security", "appsec", "infosec", "cryptograph", "penetration", "trust & safety", "trust and safety"}},
	{"Hardware", []string{"hardware", "electrical engineer", "asic", "fpga", "silicon", "mechanical engineer", "firmware", "embedded"}},
	{"Product", []string{"product manager", "product management", "program manager", "technical program", "tpm", "product owner"}},
	{"Design", []string{"designer", "design ", "ux", "ui ", "user experience", "user research", "creative"}},
	{"Data Science & Analytics", []string{"analyst", "analytics", "business intelligence"}},
	{"Sales & Business", []string{"sales", "account executive", "account manager", "business development", "partnerships", "revenue"}},
	{"Marketing", []string{"marketing", "growth", "brand", "content", "communications", "seo"}},
	{"Operations", []string{"operations", "logistics", "supply chain", "program operations", "biz ops"}},
	{"People & Recruiting", []string{"recruit", "talent", "people ops", "human resources", "hr ", "sourcer"}},
	{"Finance & Legal", []string{"finance", "accounting", "legal", "counsel", "compliance", "tax", "audit"}},
	{"Support", []string{"support", "customer success", "customer experience", "technical support", "solutions engineer", "solutions architect"}},
	{"Research", []string{"research", "scientist"}},
}

// Categorize maps a job's title and department onto a normalized set of
// categories. It always returns at least one category ("Other" as a fallback)
// and dedupes while preserving order.
func Categorize(title, department string) []string {
	hay := strings.ToLower(title + " " + department)
	var out []string
	seen := map[string]bool{}
	for _, c := range categoryKeywords {
		for _, kw := range c.keywords {
			if strings.Contains(hay, kw) {
				if !seen[c.name] {
					seen[c.name] = true
					out = append(out, c.name)
				}
				break
			}
		}
	}
	if len(out) == 0 {
		// Generic fallbacks for titles that mention a discipline but didn't hit a
		// specific keyword above.
		switch {
		case strings.Contains(hay, "engineer") || strings.Contains(hay, "engineering"):
			out = []string{"Software Engineering"}
		case strings.Contains(hay, "manager") || strings.Contains(hay, "lead") || strings.Contains(hay, "director") || strings.Contains(hay, "head of"):
			out = []string{"Operations"}
		default:
			out = []string{"Other"}
		}
	}
	return out
}

// CleanText collapses whitespace and trims a string. Useful for titles and
// locations that arrive with stray newlines or padding from the source.
func CleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
