// Package ailetter writes each application e-mail with an LLM instead of a
// hand-maintained letter template: the model reads the vacancy (full
// description included) and the candidate — their CRM record, matched
// profile and CV — and writes a German cover e-mail tailored to that one
// posting. Two backends share one prompt: Groq (groq.go) and Claude
// (claude.go).
package ailetter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
)

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

const systemPrompt = `You write job application e-mails for a recruiting agency that places candidates (mostly from Uzbekistan and Central Asia) into jobs, apprenticeships and programs in Germany. Each e-mail is sent from the candidate's own Gmail account to the employer, with the candidate's CV and other documents attached.

Write the e-mail in German, in the candidate's voice (first person), formal "Sie" register, as a plain-text e-mail body. The goal is a reply from the employer — an interview invitation or a request for more documents.

Content:
- Open with the greeting you are given, exactly as written.
- Tie the candidate to this specific posting: pick the two or three facts from their CV and profile that best match what the vacancy asks for (specialty, years and kind of experience, relevant training, German level) and say why they fit. Do not repeat the whole CV — it is attached.
- State the German level honestly as given. If the candidate does not yet live in Germany, say briefly that they are ready to relocate; do not mention visas or recognition procedures unless the CV does.
- Use only facts present in the CV or profile data. Never invent employers, certificates, training, years of experience, language levels or dates. If something the vacancy asks for is missing, leave it out rather than claiming it.
- The profile's target fields say what the candidate is applying for, not what they have done: they are not evidence of training or experience. Experience counts only where years are stated or the CV shows it.
- Do not claim to know or have done the vacancy's duties unless the CV or profile says so — no "erste Kenntnisse", "vertraut mit", "Erfahrung in" or similar without a stated source. When there is little to go on (no readable CV, no stated experience), build the letter on genuine motivation for this field and this employer, the German level, willingness to learn and readiness to relocate — honest and specific to the posting, without filler.
- Mention that the CV and documents are attached, and close with a request for a personal conversation.
- Sign off with "Mit freundlichen Grüßen", then the candidate's full name, then their phone number and e-mail on separate lines if given.
- 150 to 250 words. No placeholders, no square brackets, no markdown, no bullet lists.
- The field labels in the data below are internal notes for you. Never quote or paraphrase them in the e-mail, and never mention the agency.

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

// parseLetter decodes the model's JSON reply and rejects an empty letter.
func parseLetter(text string) (Letter, error) {
	var letter Letter
	if err := json.Unmarshal([]byte(text), &letter); err != nil {
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
	for _, s := range in.Profile.Specialties {
		line("Applies for (target field, not proof of training)", specialtyName(s.Specialty))
		if s.ExperienceYears > 0 {
			line("Stated experience", fmt.Sprintf("%d years in %s", s.ExperienceYears, specialtyName(s.Specialty)))
		}
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

// specialtyNames spells out model.SpecialtyVocabulary slugs, so the model
// reads "Zahnmedizinische Fachangestellte" rather than guessing at "zfa".
var specialtyNames = map[string]string{
	"ausbildung":     "Ausbildungsplatz (Berufsausbildung)",
	"daf_daz":        "Deutsch als Fremd-/Zweitsprache",
	"dialyse":        "Dialyse",
	"kardiologie":    "Kardiologie",
	"mfa":            "Medizinische Fachangestellte (MFA)",
	"mrt":            "MRT",
	"nephrologie":    "Nephrologie",
	"ophthalmologie": "Augenheilkunde",
	"orthopadie":     "Orthopädie",
	"pflege":         "Pflege",
	"rontgen":        "Röntgen",
	"zfa":            "Zahnmedizinische Fachangestellte (ZFA)",
}

func specialtyName(slug string) string {
	if name, ok := specialtyNames[slug]; ok {
		return name
	}
	return slug
}
