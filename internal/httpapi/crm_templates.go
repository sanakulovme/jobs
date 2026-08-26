package httpapi

import (
	"errors"
	"net/http"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
)

func (a *CRMAPI) registerTemplateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/crm/templates", a.listTemplates)
	mux.HandleFunc("POST /api/crm/templates", a.createTemplate)
	mux.HandleFunc("GET /api/crm/templates/{id}", a.getTemplate)
	mux.HandleFunc("PATCH /api/crm/templates/{id}", a.updateTemplate)
	mux.HandleFunc("DELETE /api/crm/templates/{id}", a.deleteTemplate)
	mux.HandleFunc("POST /api/crm/templates/{id}/preview", a.previewTemplate)
	mux.HandleFunc("POST /api/crm/templates/preview", a.previewDraft)
}

func (a *CRMAPI) listTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": items, "total": len(items)}, 0)
}

type templateInput struct {
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Specialty string `json:"specialty"`
	IsDefault bool   `json:"isDefault"`
}

func (in templateInput) apply(t crm.LetterTemplate) crm.LetterTemplate {
	t.Name = in.Name
	t.Subject = in.Subject
	t.Body = in.Body
	t.Specialty = in.Specialty
	t.IsDefault = in.IsDefault
	return t
}

func (a *CRMAPI) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == "" || in.Subject == "" || in.Body == "" {
		writeError(w, http.StatusBadRequest, "name, subject and body are required")
		return
	}
	if err := crm.ValidateTemplate(in.Subject, in.Body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid template: "+err.Error())
		return
	}
	t, err := a.store.CreateTemplate(in.apply(crm.LetterTemplate{}))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t, 0)
}

func (a *CRMAPI) getTemplate(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	for _, t := range items {
		if t.ID == id {
			writeJSON(w, http.StatusOK, t, 0)
			return
		}
	}
	writeError(w, http.StatusNotFound, "template not found")
}

func (a *CRMAPI) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := crm.ValidateTemplate(in.Subject, in.Body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid template: "+err.Error())
		return
	}
	t, err := a.store.UpdateTemplate(r.PathValue("id"), func(existing crm.LetterTemplate) (crm.LetterTemplate, error) {
		return in.apply(existing), nil
	})
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t, 0)
}

func (a *CRMAPI) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteTemplate(r.PathValue("id"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewContext resolves a LetterContext from optional candidateId/vacancyId
// in the request body, falling back to illustrative sample values so a
// template can be previewed before any real candidate or vacancy exists.
type previewInput struct {
	CandidateID string `json:"candidateId"`
	VacancyID   string `json:"vacancyId"`
}

func (a *CRMAPI) previewContext(in previewInput) crm.LetterContext {
	job := model.Job{
		Title:            "Medizinische Fachangestellte (m/w/d)",
		Company:          "Musterpraxis Dr. Beispiel",
		Location:         "Berlin",
		MedicalSpecialty: "Allgemeinmedizin",
	}
	candidate := crm.Candidate{FullName: "Namuna Kandidat", GermanLevel: "B2"}

	if in.VacancyID != "" {
		if j, ok := a.idx.JobByID(in.VacancyID); ok {
			job = a.withOverride(j)
		}
	}
	if in.CandidateID != "" {
		if c, err := a.store.GetCandidate(in.CandidateID); err == nil {
			candidate = c
		}
	}
	return crm.ContextFor(job, candidate)
}

// previewTemplate renders a saved template against sample or real data —
// never sends anything, just returns the rendered subject/body for review.
func (a *CRMAPI) previewTemplate(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	var tmpl crm.LetterTemplate
	found := false
	for _, t := range items {
		if t.ID == id {
			tmpl, found = t, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	var in previewInput
	if !decodeJSON(w, r, &in) {
		return
	}
	subject, body, err := crm.RenderTemplate(tmpl, a.previewContext(in))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"subject": subject, "body": body}, 0)
}

// previewDraft renders an unsaved subject/body pair — used by the template
// editor to show a live preview before the user clicks Save.
func (a *CRMAPI) previewDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		previewInput
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	subject, body, err := crm.RenderTemplate(crm.LetterTemplate{Subject: in.Subject, Body: in.Body}, a.previewContext(in.previewInput))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"subject": subject, "body": body}, 0)
}
