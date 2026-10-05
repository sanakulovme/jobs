// Package ailetter writes each application e-mail with Claude instead of a
// hand-maintained letter template: the model reads the vacancy (full
// description included) and the candidate — their CRM record, matched
// profile and, when it is a PDF, their CV — and writes a German cover e-mail
// tailored to that one posting.
package ailetter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
)

// Model is the Claude model every letter is written with.
const Model = "claude-opus-5-5"

// maxCVBytes keeps a pathological upload from blowing the 32 MB request cap
// (base64 inflates by a third); real CVs are a few hundred KB.
const maxCVBytes = 20 << 20

// Document is an uploaded file handed to the model as context.
type Document struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Input is everything one letter is written from.
type Input struct {
	Job       model.Job // the full job, description included
	Candidate crm.Candidate
	Profile   crm.ApplicationProfile
	CV        *Document // nil when the profile has no CV
}

// Letter is a ready-to-send e-mail.
type Letter struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Writer writes letters through the Claude API.
type Writer struct {
	client anthropic.Client
}

// New returns a Writer authenticated with apiKey. Extra options (a test
// server's base URL, say) are applied after the defaults.
func New(apiKey string, opts ...option.RequestOption) *Writer {
	opts = append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(3 * time.Minute),
	}, opts...)
	return &Writer{client: anthropic.NewClient(opts...)}
}

const systemPrompt = `You write job application e-mails for a recruiting agency that places candidates (mostly from Uzbekistan and Central Asia) into jobs, apprenticeships and programs in Germany. Each e-mail is sent from the candidate's own Gmail account to the employer, with the candidate's CV and other documents attached.

Write the e-mail in German, in the candidate's voice (first person), formal "Sie" register, as a plain-text e-mail body. The goal is a reply from the employer — an interview invitation or a request for more documents.

Content:
- Open with the greeting you are given, exactly as written.
- Tie the candidate to this specific posting: pick the two or three facts from their CV and profile that best match what the vacancy asks for (specialty, years and kind of experience, relevant training, German level) and say why they fit. Do not repeat the whole CV — it is attached.
- State the German level honestly as given. If the candidate does not yet live in Germany, say briefly that they are ready to relocate; do not mention visas or recognition procedures unless the CV does.
- Use only facts present in the CV or profile data. Never invent employers, certificates, years of experience, language levels or dates. If something the vacancy asks for is missing, leave it out rather than claiming it.
- Mention that the CV and documents are attached, and close with a request for a personal conversation.
- Sign off with "Mit freundlichen Grüßen", then the candidate's full name, then their phone number and e-mail on separate lines if given.
- 150 to 250 words. No placeholders, no square brackets, no markdown, no bullet lists.

The subject is a short German subject line, normally "Bewerbung als <job title> – <candidate full name>", adjusted if the title is very long.`

var letterSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"subject": map[string]any{"type": "string", "description": "E-mail subject line"},
		"body":    map[string]any{"type": "string", "description": "Plain-text e-mail body"},
	},
	"required":             []string{"subject", "body"},
	"additionalProperties": false,
}

// Write produces the subject and body for one application.
func (w *Writer) Write(ctx context.Context, in Input) (Letter, error) {
	var content []anthropic.BetaContentBlockParamUnion

	// The CV comes first and carries the cache breakpoint: one run usually
	// writes several letters for the same candidate, and every one after the
	// first then reads the system prompt + CV from cache.
	cvNote := "No CV is attached; rely on the profile data only."
	if cv := in.CV; cv != nil && isPDF(cv) && len(cv.Data) <= maxCVBytes {
		doc := anthropic.NewBetaDocumentBlock(anthropic.BetaBase64PDFSourceParam{
			Data: base64.StdEncoding.EncodeToString(cv.Data),
		})
		doc.OfDocument.Title = param.NewOpt("CV: " + cv.Filename)
		doc.OfDocument.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
		content = append(content, doc)
		cvNote = "The candidate's CV is the attached document."
	} else if in.CV != nil {
		cvNote = fmt.Sprintf("The CV (%s) is attached to the e-mail but is not a PDF, so you cannot read it; rely on the profile data only.", in.CV.Filename)
	}
	content = append(content, anthropic.NewBetaTextBlock(cvNote+"\n\n"+describe(in)))

	resp, err := w.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     Model,
		MaxTokens: 16000,
		System: []anthropic.BetaTextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(content...)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: letterSchema},
		},
		// If a safety classifier declines the request, the API re-serves it
		// on a suitable fallback model inside the same call.
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return Letter{}, fmt.Errorf("AI xat yozolmadi: %w", err)
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return Letter{}, errors.New("AI bu xatni yozishni rad etdi")
	case anthropic.BetaStopReasonMaxTokens:
		return Letter{}, errors.New("AI javobi chegaraga yetib kesildi")
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	var letter Letter
	if err := json.Unmarshal([]byte(text.String()), &letter); err != nil {
		return Letter{}, fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
	}
	letter.Subject = strings.TrimSpace(letter.Subject)
	letter.Body = strings.TrimSpace(letter.Body)
	if letter.Subject == "" || letter.Body == "" {
		return Letter{}, errors.New("AI bo'sh xat qaytardi")
	}
	return letter, nil
}

func isPDF(d *Document) bool {
	return strings.EqualFold(d.ContentType, "application/pdf") ||
		strings.HasSuffix(strings.ToLower(d.Filename), ".pdf")
}

// describe renders the candidate and vacancy as labelled plain text,
// skipping empty fields so the model never sees blank slots to fill in.
func describe(in Input) string {
	var b strings.Builder
	line := func(label, value string) {
		if v := strings.TrimSpace(value); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, v)
		}
	}

	c := in.Candidate
	b.WriteString("<candidate>\n")
	line("Full name", c.FullName)
	line("E-mail", c.ContactEmail)
	line("Phone", c.Phone)
	line("German level", c.GermanLevel)
	line("Citizenship", c.Citizenship)
	line("Currently lives in", c.CurrentCountry)
	line("Direction", c.Direction)
	for _, s := range in.Profile.Specialties {
		exp := ""
		if s.ExperienceYears > 0 {
			exp = fmt.Sprintf(" (%d years experience)", s.ExperienceYears)
		}
		line("Specialty", s.Specialty+exp)
	}
	line("Recruiter notes", c.Notes)
	b.WriteString("</candidate>\n\n")

	j := in.Job
	b.WriteString("<vacancy>\n")
	line("Job title", j.Title)
	line("Employer", j.Company)
	line("Location", j.Location)
	line("Medical specialty", j.MedicalSpecialty)
	line("Description", j.Description)
	line("Main duties", j.MainDuties)
	line("Requirements", j.MandatoryRequirements)
	line("Nice to have", j.PreferredRequirements)
	b.WriteString("</vacancy>\n\n")

	fmt.Fprintf(&b, "Greeting to open with: %s\n", crm.Greeting(j))
	return b.String()
}
