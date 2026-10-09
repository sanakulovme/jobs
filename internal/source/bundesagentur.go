package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"faangjobs/internal/model"
	"faangjobs/internal/registry"
)

// bundesagentur adapts the Bundesagentur für Arbeit Jobsuche API
// (rest.arbeitsagentur.de) — a public REST API used by the official
// arbeitsagentur.de job board (X-API-Key "jobboerse-jobsuche" is the public
// client id documented at https://jobsuche.api.bund.dev/, no account needed).
//
// Unlike the generic path-based adapter, this one needs two requests per job:
// the search endpoint lists only basic fields, and a second per-job "detail"
// call returns the free-text posting. Everything not directly present in the
// API (email, contact person, required German level, driver's-license/car
// mentions, duties vs. requirements, ...) is extracted from that free text
// with best-effort regex heuristics — these fields are frequently empty when
// a posting simply doesn't mention them.
//
// Recognized Config keys (all optional): was, wo, umkreis, maxJobs, concurrency.
type bundesagentur struct{}

func init() { Register(bundesagentur{}) }

func (bundesagentur) Kind() string { return "bundesagentur" }

const (
	baAPIKey     = "jobboerse-jobsuche"
	baListURL    = "https://rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v6/jobs"
	baDetailURL  = "https://rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v4/jobdetails/%s"
	baDetailPage = "https://www.arbeitsagentur.de/jobsuche/jobdetail/%s"
)

type baListResponse struct {
	Ergebnisliste []baListJob `json:"ergebnisliste"`
	MaxErgebnisse int         `json:"maxErgebnisse"`
}

type baListJob struct {
	Titel             string `json:"stellenangebotsTitel"`
	Firma             string `json:"firma"`
	Referenznummer    string `json:"referenznummer"`
	Hauptberuf        string `json:"hauptberuf"`
	Veroeffentlicht   string `json:"datumErsteVeroeffentlichung"`
	Stellenlokationen []struct {
		Adresse struct {
			Ort    string `json:"ort"`
			Region string `json:"region"` // Bundesland, upper-case ("NORDRHEIN-WESTFALEN")
			Land   string `json:"land"`   // "DEUTSCHLAND" for domestic postings
		} `json:"adresse"`
	} `json:"stellenlokationen"`
}

type baDetailResponse struct {
	Beschreibung              string `json:"stellenangebotsBeschreibung"`
	ArbeitgeberdarstellungURL string `json:"arbeitgeberdarstellungUrl"`
}

func (bundesagentur) Fetch(ctx context.Context, f *Fetcher, c registry.Company) ([]model.Job, error) {
	was := configStr(c, "was", "Medizinische Fachangestellte")
	wo := configStr(c, "wo", "Deutschland")
	umkreis := configInt(c, "umkreis", 200)
	maxJobs := configInt(c, "maxJobs", 2000)
	concurrency := configInt(c, "concurrency", 8)

	list, err := baFetchList(ctx, f, was, wo, umkreis, maxJobs)
	if err != nil {
		return nil, err
	}

	jobs := make([]model.Job, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, item := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item baListJob) {
			defer wg.Done()
			defer func() { <-sem }()
			jobs[i] = baBuildJob(ctx, f, c, item)
		}(i, item)
	}
	wg.Wait()

	// Only postings that name an application email are useful downstream (see
	// model.Job.ApplicationEmail); the rest (usually "apply by mail/in person")
	// are dropped here rather than stored.
	kept := jobs[:0]
	for _, j := range jobs {
		if j.ApplicationEmail != "" {
			kept = append(kept, j)
		}
	}

	return finalizeResult(c, kept)
}

func baFetchList(ctx context.Context, f *Fetcher, was, wo string, umkreis, maxJobs int) ([]baListJob, error) {
	const pageSize = 25
	headers := map[string]string{"X-API-Key": baAPIKey}
	var all []baListJob
	for page := 1; len(all) < maxJobs; page++ {
		url := fmt.Sprintf("%s?was=%s&wo=%s&umkreis=%d&page=%d&size=%d",
			baListURL, urlEncode(was), urlEncode(wo), umkreis, page, pageSize)
		var resp baListResponse
		if err := f.GetJSON(ctx, url, headers, &resp); err != nil {
			if len(all) > 0 {
				break
			}
			return nil, err
		}
		if len(resp.Ergebnisliste) == 0 {
			break
		}
		all = append(all, resp.Ergebnisliste...)
		if len(all) >= resp.MaxErgebnisse || len(resp.Ergebnisliste) < pageSize {
			break
		}
	}
	if len(all) > maxJobs {
		all = all[:maxJobs]
	}
	return all, nil
}

func baBuildJob(ctx context.Context, f *Fetcher, c registry.Company, item baListJob) model.Job {
	city := ""
	if len(item.Stellenlokationen) > 0 {
		city = baLocation(item.Stellenlokationen[0].Adresse.Ort, item.Stellenlokationen[0].Adresse.Region, item.Stellenlokationen[0].Adresse.Land)
	}
	j := model.Job{
		ID:               c.ID + "~" + model.StableID(item.Referenznummer),
		Company:          strings.TrimSpace(item.Firma),
		Title:            strings.TrimSpace(item.Titel),
		Location:         city,
		URL:              fmt.Sprintf(baDetailPage, item.Referenznummer),
		PostedAt:         parseTime(item.Veroeffentlicht),
		UpdatedAt:        parseTime(item.Veroeffentlicht),
		ReferenceNumber:  item.Referenznummer,
		MedicalSpecialty: baClassifySpecialty(item.Titel, item.Hauptberuf),
		Specialties:      baClassifySpecialties(item.Titel, item.Hauptberuf, ""),
	}

	headers := map[string]string{"X-API-Key": baAPIKey}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(item.Referenznummer))
	var d baDetailResponse
	if err := f.GetJSON(ctx, fmt.Sprintf(baDetailURL, encoded), headers, &d); err == nil {
		desc := baCleanDescription(d.Beschreibung)
		j.Description = desc
		j.Website = baFirstURL(desc, "arbeitsagentur.de")
		if d.ArbeitgeberdarstellungURL != "" {
			j.Website = d.ArbeitgeberdarstellungURL
		}
		j.ApplicationEmail = baFirstEmail(desc)
		j.ContactPerson, j.Salutation = baContactPerson(desc)
		j.RequiredGermanLevel = baGermanLevel(desc)
		j.RequiresDriversLicense = baContainsAny(desc, "führerschein")
		j.RequiresOwnCar = baContainsAny(desc, "eigenen pkw", "eigenes auto", "eigenen wagen", "eigenem pkw")
		j.RequiresGermanMFATraining = baContainsAny(desc, "ausbildung als medizinische", "ausbildung zur medizinischen", "abgeschlossene mfa", "mfa-ausbildung")
		j.MainDuties = baSection(desc, []string{"ihre aufgaben", "aufgaben"})
		j.MandatoryRequirements = baSection(desc, []string{"voraussetzungen", "anforderungen", "ihr profil", "was sie mitbringen"})
		j.PreferredRequirements = baSection(desc, []string{"wünschenswert", "von vorteil", "vorteilhaft"})
		if j.MandatoryRequirements != "" {
			j.RequiredQualifications = baBullets(j.MandatoryRequirements)
		}
		// Re-classify with the full description in hand — a specialty is
		// sometimes only mentioned in the body text, not the title.
		j.Specialties = baClassifySpecialties(item.Titel, item.Hauptberuf, desc)
	}
	return j
}

func urlEncode(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "+"), "&", "%26")
}

// baLocation renders a posting's place as "Ort, Bundesland, Germany" (the
// Bundesland from the API, so the board's region filter can drill into it
// without guessing from the town name). Postings abroad keep their country.
func baLocation(ort, region, land string) string {
	parts := []string{strings.TrimSpace(ort)}
	if st := model.GermanStateName(region); st != "" {
		parts = append(parts, st)
	}
	switch l := strings.TrimSpace(land); {
	case l == "" || strings.EqualFold(l, "deutschland"):
		parts = append(parts, "Germany")
	default:
		r := []rune(strings.ToLower(l))
		r[0] = unicode.ToUpper(r[0])
		parts = append(parts, string(r))
	}
	if parts[0] == "" {
		parts = parts[1:]
	}
	return strings.Join(parts, ", ")
}

// ── extraction heuristics (best-effort over free-text German descriptions) ──

var (
	baEmailRe     = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	baURLRe       = regexp.MustCompile(`https?://[^\s)>\]]+|www\.[a-zA-Z0-9-]+\.[a-zA-Z]{2,}[^\s)>\]]*`)
	baGermanLvlRe = regexp.MustCompile(`\b([ABC][12])\b`)
	baContactRe   = regexp.MustCompile(`(Herrn?|Frau)\s+([A-ZÄÖÜ][a-zäöüß]+(?:\s+[A-ZÄÖÜ][a-zäöüß]+){0,2})`)

	baSectionHeadings = []string{"ihre aufgaben", "aufgaben", "voraussetzungen", "anforderungen", "ihr profil",
		"was sie mitbringen", "wünschenswert", "von vorteil", "vorteilhaft", "wir bieten", "ihre vorteile"}
)

// baCleanDescription strips markdown emphasis markers, keeping line breaks so
// baSection can still split on heading lines.
func baCleanDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "###", "")
	return strings.TrimSpace(s)
}

func baFirstEmail(s string) string { return baEmailRe.FindString(s) }

func baFirstURL(s, excludeHost string) string {
	for _, m := range baURLRe.FindAllString(s, -1) {
		if !strings.Contains(strings.ToLower(m), excludeHost) {
			return strings.TrimRight(m, ".,;)")
		}
	}
	return ""
}

func baGermanLevel(s string) string { return baGermanLvlRe.FindString(s) }

func baContactPerson(s string) (name, salutation string) {
	m := baContactRe.FindStringSubmatch(s)
	if m == nil {
		return "", ""
	}
	salutation = "Frau"
	if strings.HasPrefix(m[1], "Herr") {
		salutation = "Herr"
	}
	return m[2], salutation
}

func baContainsAny(hay string, needles ...string) bool {
	l := strings.ToLower(hay)
	for _, n := range needles {
		if strings.Contains(l, n) {
			return true
		}
	}
	return false
}

// baSection extracts the text following a heading line matching any of
// startKeys (case-insensitive) up to the next heading line, best-effort. The
// source text has no consistent structure, so this returns "" when no
// matching heading is found.
func baSection(desc string, startKeys []string) string {
	lines := strings.Split(desc, "\n")
	startIdx := -1
	for i, line := range lines {
		l := strings.ToLower(strings.Trim(strings.TrimSpace(line), ":"))
		for _, k := range startKeys {
			if l == k || strings.HasPrefix(l, k) {
				startIdx = i + 1
				break
			}
		}
		if startIdx != -1 {
			break
		}
	}
	if startIdx == -1 {
		return ""
	}
	var out []string
	for i := startIdx; i < len(lines); i++ {
		l := strings.ToLower(strings.Trim(strings.TrimSpace(lines[i]), ":"))
		isHeading := false
		for _, h := range baSectionHeadings {
			if l == h || (l != "" && strings.HasPrefix(l, h) && len(l) < len(h)+3) {
				isHeading = true
				break
			}
		}
		if isHeading {
			break
		}
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, strings.TrimSpace(lines[i]))
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// baBullets turns "- foo" / "* foo" lines from a section into a list of
// strings (rendered as JSON in the model.Job.RequiredQualifications field).
func baBullets(section string) []string {
	var items []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			items = append(items, strings.TrimSpace(line[2:]))
		}
	}
	return items
}

// baSpecialtyKeywords maps title/profession keywords to a German medical
// specialty label. Best-effort classification, not sourced from the API (the
// search API only ever returns "Medizinische/r Fachangestellte/r" as the
// profession for this query; the specialty comes from the employer's
// practice type as it appears in the title).
var baSpecialtyKeywords = []struct {
	specialty string
	keywords  []string
}{
	{"Allgemeinmedizin", []string{"allgemeinmedizin", "hausarzt", "hausärzt"}},
	{"Zahnmedizin", []string{"zahnarzt", "zahnärzt", "zfa", "kieferorthop"}},
	{"Augenheilkunde", []string{"augenarzt", "augenheilkunde", "ophthalmolog"}},
	{"Gynäkologie", []string{"gynäkolog", "frauenarzt", "frauenheilkunde"}},
	{"Pädiatrie", []string{"kinderarzt", "kinder- und jugendmedizin", "pädiatr"}},
	{"Dermatologie", []string{"dermatolog", "hautarzt"}},
	{"Orthopädie", []string{"orthopäd"}},
	{"HNO", []string{"hno", "hals-nasen-ohren"}},
	{"Innere Medizin", []string{"internist", "innere medizin"}},
	{"Chirurgie", []string{"chirurg"}},
	{"Radiologie", []string{"radiolog", "röntgen"}},
	{"Urologie", []string{"urolog"}},
	{"Psychiatrie", []string{"psychiatr", "psychotherap"}},
	{"Labormedizin", []string{"labor"}},
	{"Neurologie", []string{"neurolog"}},
}

func baClassifySpecialty(title, hauptberuf string) string {
	hay := strings.ToLower(title + " " + hauptberuf)
	for _, s := range baSpecialtyKeywords {
		for _, kw := range s.keywords {
			if strings.Contains(hay, kw) {
				return s.specialty
			}
		}
	}
	return ""
}

// baSlugKeywords maps each model.SpecialtyVocabulary slug to the German
// substrings that indicate it, searched over title+profession+description.
// "mfa" matches whenever the base profession does, since every posting this
// adapter fetches is already an MFA search result.
var baSlugKeywords = map[string][]string{
	"mfa":            {"medizinische fachangestellte", "medizinischer fachangestellter", " mfa "},
	"zfa":            {"zahnmedizinische fachangestellte", " zfa ", "zahnarzt", "zahnärzt"},
	"ausbildung":     {"ausbildung", "auszubildende", "azubi"},
	"daf_daz":        {"daf/daz", "daf / daz", "deutsch als fremdsprache", "deutsch als zweitsprache"},
	"dialyse":        {"dialyse", "nephrolog"},
	"nephrologie":    {"nephrolog"},
	"kardiologie":    {"kardiolog", "herz- und gefäß", "kardio"},
	"mrt":            {"mrt", "magnetresonanztomogr", "kernspintomogr"},
	"rontgen":        {"röntgen", "radiolog", "nuklearmedizin"},
	"ophthalmologie": {"augenarzt", "augenheilkunde", "ophthalmolog"},
	"orthopadie":     {"orthopäd"},
	"pflege":         {"pflege", "krankenpfleg", "altenpfleg"},
}

// baTitleOnlySlugs are decided from title+profession alone. "ausbildung"
// marks a posting that IS an apprenticeship; descriptions of ordinary jobs
// mention the word constantly as a requirement ("abgeschlossene Ausbildung
// als MFA"), which tagged qualified-staff jobs as apprenticeships and matched
// them to candidates looking for an Ausbildungsplatz.
var baTitleOnlySlugs = map[string]bool{"ausbildung": true}

// baClassifySpecialties returns every model.SpecialtyVocabulary slug whose
// keywords appear in title+profession+description (title+profession only
// for baTitleOnlySlugs), best-effort — a posting that names no recognizable
// specialty beyond the base MFA profession simply gets ["mfa"] (or nothing,
// if even that phrase is absent from the text).
func baClassifySpecialties(title, hauptberuf, desc string) []string {
	head := strings.ToLower(title + " " + hauptberuf)
	full := head + " " + strings.ToLower(desc)
	var out []string
	for _, slug := range model.SpecialtyVocabulary {
		hay := full
		if baTitleOnlySlugs[slug] {
			hay = head
		}
		for _, kw := range baSlugKeywords[slug] {
			if strings.Contains(hay, kw) {
				out = append(out, slug)
				break
			}
		}
	}
	return out
}
