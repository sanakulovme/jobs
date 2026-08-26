package crm

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"faangjobs/internal/model"
)

// LetterContext supplies the values for every {{token}} a template may use.
type LetterContext struct {
	VacancyTitle         string
	Employer             string
	City                 string
	MedicalSpecialty     string
	CandidateName        string
	CandidateGermanLevel string
	Greeting             string
}

// ContextFor builds a LetterContext from a matched vacancy + candidate,
// deriving Greeting the same way the bundesagentur adapter already extracts
// a contact person: "Sehr geehrte{r} {Salutation} {Name}," when the posting
// named one, else the generic "Sehr geehrte Damen und Herren,".
func ContextFor(job model.Job, candidate Candidate) LetterContext {
	greeting := "Sehr geehrte Damen und Herren,"
	if job.ContactPerson != "" {
		suffix := ""
		if strings.EqualFold(job.Salutation, "Herr") {
			suffix = "r"
		}
		greeting = fmt.Sprintf("Sehr geehrte%s %s %s,", suffix, job.Salutation, job.ContactPerson)
	}
	return LetterContext{
		VacancyTitle:         job.Title,
		Employer:             job.Company,
		City:                 job.Location,
		MedicalSpecialty:     job.MedicalSpecialty,
		CandidateName:        candidate.FullName,
		CandidateGermanLevel: candidate.GermanLevel,
		Greeting:             greeting,
	}
}

// funcMap exposes each supported token as a zero-arg template function, so a
// template body written as "{{vacancy_title}}" (the reference app's exact
// token spelling, no leading dot) parses as a valid text/template call.
func funcMap(ctx LetterContext) template.FuncMap {
	return template.FuncMap{
		"vacancy_title":          func() string { return ctx.VacancyTitle },
		"employer":               func() string { return ctx.Employer },
		"city":                   func() string { return ctx.City },
		"medical_specialty":      func() string { return ctx.MedicalSpecialty },
		"candidate_name":         func() string { return ctx.CandidateName },
		"candidate_german_level": func() string { return ctx.CandidateGermanLevel },
		"greeting":               func() string { return ctx.Greeting },
	}
}

// ValidateTemplate parses subject+body against the supported token set,
// returning an error naming the first unrecognized token — called at save
// time so a typo'd token fails immediately rather than rendering blank later.
func ValidateTemplate(subject, body string) error {
	ctx := LetterContext{}
	if _, err := parse(subject, ctx); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if _, err := parse(body, ctx); err != nil {
		return fmt.Errorf("body: %w", err)
	}
	return nil
}

// RenderTemplate fills a template's subject and body with ctx.
func RenderTemplate(t LetterTemplate, ctx LetterContext) (subject, body string, err error) {
	subject, err = parse(t.Subject, ctx)
	if err != nil {
		return "", "", fmt.Errorf("subject: %w", err)
	}
	body, err = parse(t.Body, ctx)
	if err != nil {
		return "", "", fmt.Errorf("body: %w", err)
	}
	return subject, body, nil
}

func parse(text string, ctx LetterContext) (string, error) {
	tmpl, err := template.New("letter").Funcs(funcMap(ctx)).Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// TemplateFor picks the best letter template for a vacancy: one narrowed to
// a matched specialty, else the default, else the oldest — the same
// fallback chain as the reference app's AutoApplyService::templateFor().
func TemplateFor(templates []LetterTemplate, job model.Job) (LetterTemplate, bool) {
	wanted := make(map[string]bool, len(job.Specialties))
	for _, s := range job.Specialties {
		wanted[s] = true
	}
	for _, t := range templates {
		if t.Specialty != "" && wanted[t.Specialty] {
			return t, true
		}
	}
	for _, t := range templates {
		if t.IsDefault {
			return t, true
		}
	}
	var oldest LetterTemplate
	found := false
	for _, t := range templates {
		if !found || t.CreatedAt.Before(oldest.CreatedAt) {
			oldest, found = t, true
		}
	}
	return oldest, found
}
