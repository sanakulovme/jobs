package source

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"faangjobs/internal/model"
	"faangjobs/internal/registry"
)

// Helpers for scraping arbitrary web pages (the CRM's "any site" scrape,
// internal/httpapi/crm_scrape_site.go), where an LLM reads the page instead
// of a dedicated adapter parsing a known API.

var (
	pageDropRe    = regexp.MustCompile(`(?is)<(script|style|noscript|svg|template|iframe|head)\b.*?</(script|style|noscript|svg|template|iframe|head)\s*>`)
	pageCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	pageLinkRe    = regexp.MustCompile(`(?is)<a\b[^>]*?\bhref\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))[^>]*>(.*?)</a\s*>`)
	pageBlockRe   = regexp.MustCompile(`(?i)<(br|/p|p|/div|div|/li|li|/tr|tr|/h[1-6]|h[1-6]|/section|section|/article|article|/ul|/ol|hr)\b[^>]*>`)
	pageTagRe     = regexp.MustCompile(`(?s)<[^>]+>`)
	pageSpaceRe   = regexp.MustCompile(`[ \t\f\v\r\x{00a0}]+`)
	pageBlankRe   = regexp.MustCompile(`\n\s*\n+`)
)

// PageText turns an HTML page into readable plain text for an LLM: scripts,
// styles and markup are dropped, block elements become line breaks, and
// every link is kept as "text [link: absolute-URL]" (mailto: links become
// the bare address) so the model can point at each posting's own page.
func PageText(src string, base *url.URL) string {
	src = pageDropRe.ReplaceAllString(src, " ")
	src = pageCommentRe.ReplaceAllString(src, " ")
	src = pageLinkRe.ReplaceAllStringFunc(src, func(m string) string {
		sub := pageLinkRe.FindStringSubmatch(m)
		href := html.UnescapeString(strings.TrimSpace(sub[2] + sub[3] + sub[4]))
		text := strings.TrimSpace(pageTagRe.ReplaceAllString(sub[5], " "))
		lower := strings.ToLower(href)
		switch {
		case strings.HasPrefix(lower, "mailto:"):
			addr, _, _ := strings.Cut(href[len("mailto:"):], "?")
			return " " + text + " " + addr + " "
		case href == "", strings.HasPrefix(href, "#"), strings.HasPrefix(lower, "javascript:"), strings.HasPrefix(lower, "tel:"):
			return " " + text + " "
		}
		if u, err := base.Parse(href); err == nil {
			href = u.String()
		}
		if text == "" {
			return " "
		}
		return " " + text + " [link: " + href + "] "
	})
	src = pageBlockRe.ReplaceAllString(src, "\n")
	src = pageTagRe.ReplaceAllString(src, " ")
	src = html.UnescapeString(src)

	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(pageSpaceRe.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(pageBlankRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// Mailbox local parts that are never where an application should go, and
// ones that very likely are.
var (
	emailAvoid  = []string{"datenschutz", "privacy", "noreply", "no-reply", "donotreply", "support", "webmaster", "abuse", "presse", "press", "dsb", "impressum"}
	emailPrefer = []string{"bewerb", "karriere", "career", "jobs", "job", "recruit", "personal", "hr", "ausbildung", "stellen"}
)

// Emails returns every e-mail address in text.
func Emails(text string) []string {
	var out []string
	for _, e := range baEmailRe.FindAllString(text, -1) {
		out = append(out, strings.TrimRight(e, "."))
	}
	return out
}

// PickApplicationEmail returns the address in text most likely meant for
// applications: one whose local part says so (bewerbung@, karriere@, hr@…)
// if present, else the first that isn't a privacy/no-reply/support address.
// Addresses in exclude (lower-cased) are never returned.
func PickApplicationEmail(text string, exclude map[string]bool) string {
	first := ""
	for _, e := range Emails(text) {
		lower := strings.ToLower(e)
		local := lower[:strings.Index(lower, "@")]
		if exclude[lower] || containsAnyOf(local, emailAvoid) || strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") {
			continue
		}
		if containsAnyOf(local, emailPrefer) {
			return e
		}
		if first == "" {
			first = e
		}
	}
	return first
}

func containsAnyOf(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ClassifySpecialties tags free text with model.SpecialtyVocabulary slugs —
// the same keyword table the Bundesagentur adapter uses, so CRM matching
// treats postings from any site alike.
func ClassifySpecialties(text string) []string { return baClassifySpecialties("", "", text) }

// ContactPerson finds a "Frau/Herr Name" contact in text, as the
// Bundesagentur adapter does for its postings.
func ContactPerson(text string) (name, salutation string) { return baContactPerson(text) }

// Finalize applies the shared adapter post-processing (company fields,
// categories, ids, cleanup, dropping title- or URL-less jobs) to jobs that
// were assembled outside an adapter.
func Finalize(c registry.Company, jobs []model.Job) []model.Job { return finalize(c, jobs) }
