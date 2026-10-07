package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// PageJob is one posting the model found on a web page. Fields the page
// doesn't show are empty strings, never guesses.
type PageJob struct {
	Title            string `json:"title"`
	Employer         string `json:"employer"`
	Location         string `json:"location"`
	URL              string `json:"url"`
	ApplicationEmail string `json:"applicationEmail"`
	Summary          string `json:"summary"`
}

// MaxPageChars bounds the page text sent to the model; job-board listing
// pages that matter fit comfortably, and the prompt says when it was cut.
const MaxPageChars = 60000

const jobsSystemPrompt = `You read the text of one web page — a company careers page, a job board listing, or a single job posting — and list the job postings on it. Links in the text appear as "text [link: URL]".

Rules:
- One entry per individual posting (job, apprenticeship/Ausbildung, internship, au-pair placement, language course offer, etc.) actually shown on the page. If the page is a single posting, return just that one. If there are none, return an empty list.
- Ignore navigation, filters, ads, cookie banners, "similar jobs" teasers without a title, and the site's own corporate links.
- title: the posting's title as written. employer: the hiring organisation if shown (else empty). location: city/region as written.
- url: the link that opens this posting's own page, copied exactly from a [link: ...] marker. Empty if there is none.
- applicationEmail: an e-mail address the page gives for applying to this posting. Never a generic privacy/support/noreply address, and never one you made up. Empty if none is shown.
- summary: at most two short sentences in the page's language, taken from what the page says about the posting. Empty if the page shows only the title.
- List at most 40 postings, in page order. A posting linked twice (title and "more" link) is one posting.
- Copy, don't invent. Leave a field empty rather than guessing.`

var pageJobsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"jobs": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":            map[string]any{"type": "string"},
					"employer":         map[string]any{"type": "string"},
					"location":         map[string]any{"type": "string"},
					"url":              map[string]any{"type": "string"},
					"applicationEmail": map[string]any{"type": "string"},
					"summary":          map[string]any{"type": "string"},
				},
				"required":             []string{"title", "employer", "location", "url", "applicationEmail", "summary"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"jobs"},
	"additionalProperties": false,
}

// jobsUserPrompt renders the page for the model, saying so when it was cut.
func jobsUserPrompt(pageURL, pageText string) string {
	note := ""
	if len(pageText) > MaxPageChars {
		pageText = pageText[:MaxPageChars]
		note = "\n(The page text was cut at this point; list only the postings above.)"
	}
	return fmt.Sprintf("Page URL: %s\n\n<page>\n%s%s\n</page>", pageURL, pageText, note)
}

// parsePageJobs decodes the model's reply and drops entries without a title.
func parsePageJobs(text string) ([]PageJob, error) {
	var out struct {
		Jobs []PageJob `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
	}
	jobs := out.Jobs[:0]
	for _, j := range out.Jobs {
		j.Title = strings.TrimSpace(j.Title)
		if j.Title == "" {
			continue
		}
		j.Employer = strings.TrimSpace(j.Employer)
		j.Location = strings.TrimSpace(j.Location)
		j.URL = strings.TrimSpace(j.URL)
		j.ApplicationEmail = strings.TrimSpace(j.ApplicationEmail)
		j.Summary = strings.TrimSpace(j.Summary)
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// ExtractJobs lists the postings on a page (Groq).
func (g *Groq) ExtractJobs(ctx context.Context, pageURL, pageText string) ([]PageJob, error) {
	text, err := g.chatJSON(ctx, jobsSystemPrompt, jobsUserPrompt(pageURL, pageText), "page_jobs", pageJobsSchema)
	if err != nil {
		return nil, err
	}
	return parsePageJobs(text)
}

// ExtractJobs lists the postings on a page (Claude).
func (w *Claude) ExtractJobs(ctx context.Context, pageURL, pageText string) ([]PageJob, error) {
	text, err := w.messageJSON(ctx, jobsSystemPrompt,
		[]anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(jobsUserPrompt(pageURL, pageText))},
		pageJobsSchema)
	if err != nil {
		return nil, err
	}
	return parsePageJobs(text)
}
