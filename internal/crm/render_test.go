package crm

import (
	"strings"
	"testing"

	"faangjobs/internal/model"
)

func TestRenderTemplateAllTokens(t *testing.T) {
	tmpl := LetterTemplate{
		Subject: "Bewerbung als {{vacancy_title}}",
		Body: "{{greeting}}\n\n" +
			"Ich bewerbe mich bei {{employer}} in {{city}} als {{vacancy_title}}. " +
			"Fachrichtung: {{medical_specialty}}. Deutschniveau: {{candidate_german_level}}.\n\n" +
			"{{candidate_name}}",
	}
	job := model.Job{
		Title: "Medizinische Fachangestellte", Company: "Praxis Mustermann", Location: "Berlin",
		MedicalSpecialty: "Kardiologie", ContactPerson: "Mustermann", Salutation: "Herr",
	}
	candidate := Candidate{FullName: "Abbos Sanakulov", GermanLevel: "B2"}

	subject, body, err := RenderTemplate(tmpl, ContextFor(job, candidate))
	if err != nil {
		t.Fatalf("RenderTemplate failed: %v", err)
	}
	if subject != "Bewerbung als Medizinische Fachangestellte" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"Praxis Mustermann", "Berlin", "Kardiologie", "B2", "Abbos Sanakulov", "Sehr geehrter Herr Mustermann,"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderTemplateFallbackGreeting(t *testing.T) {
	tmpl := LetterTemplate{Subject: "S", Body: "{{greeting}}"}
	job := model.Job{} // no ContactPerson
	_, body, err := RenderTemplate(tmpl, ContextFor(job, Candidate{}))
	if err != nil {
		t.Fatalf("RenderTemplate failed: %v", err)
	}
	if body != "Sehr geehrte Damen und Herren," {
		t.Errorf("body = %q", body)
	}
}

func TestValidateTemplateRejectsUnknownToken(t *testing.T) {
	if err := ValidateTemplate("{{vacancy_title}}", "{{typo_token}}"); err == nil {
		t.Fatal("expected an error for an unrecognized token, got nil")
	}
}

func TestValidateTemplateAcceptsKnownTokens(t *testing.T) {
	body := "{{greeting}} {{employer}} {{city}} {{medical_specialty}} {{candidate_german_level}} {{candidate_name}}"
	if err := ValidateTemplate("{{vacancy_title}}", body); err != nil {
		t.Fatalf("expected known tokens to validate, got %v", err)
	}
}

func TestTemplateForPrefersSpecialtyMatch(t *testing.T) {
	generic := LetterTemplate{ID: "generic", IsDefault: true}
	cardio := LetterTemplate{ID: "cardio", Specialty: "kardiologie"}
	job := model.Job{Specialties: []string{"mfa", "kardiologie"}}

	got, ok := TemplateFor([]LetterTemplate{generic, cardio}, job)
	if !ok || got.ID != "cardio" {
		t.Fatalf("expected specialty-matched template, got %+v (ok=%v)", got, ok)
	}
}

func TestTemplateForFallsBackToDefault(t *testing.T) {
	generic := LetterTemplate{ID: "generic", IsDefault: true}
	cardio := LetterTemplate{ID: "cardio", Specialty: "kardiologie"}
	job := model.Job{Specialties: []string{"mfa"}} // no kardiologie match

	got, ok := TemplateFor([]LetterTemplate{generic, cardio}, job)
	if !ok || got.ID != "generic" {
		t.Fatalf("expected default template, got %+v (ok=%v)", got, ok)
	}
}

func TestTemplateForEmptyReturnsFalse(t *testing.T) {
	if _, ok := TemplateFor(nil, model.Job{}); ok {
		t.Fatal("expected ok=false for an empty template list")
	}
}
