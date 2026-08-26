package httpapi

import (
	"net/http"
	"strings"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
)

func (a *CRMAPI) registerVacancyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/crm/vacancies", a.listVacancies)
	mux.HandleFunc("POST /api/crm/vacancies", a.createVacancy)
	mux.HandleFunc("POST /api/crm/vacancies/{jobId}/specialties", a.setVacancySpecialties)
	mux.HandleFunc("GET /api/crm/vacancies/{jobId}/matches", a.vacancyMatches)
}

// withOverride applies a CRM-set Specialties correction over a crawled job's
// own classification, if an operator has recorded one — see
// crm.Store.SetVacancyOverride.
func (a *CRMAPI) withOverride(job model.Job) model.Job {
	if override, ok, err := a.store.VacancyOverride(job.ID); err == nil && ok {
		job.Specialties = override
	}
	return job
}

func (a *CRMAPI) listVacancies(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	specialty := r.URL.Query().Get("specialty")
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 25)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 25
	}

	snap := a.idx.Snapshot()
	var matched []model.Job
	for _, j := range snap.jobs {
		j = a.withOverride(j)
		if q != "" && !strings.Contains(strings.ToLower(j.Title+" "+j.Company+" "+j.Location), q) {
			continue
		}
		if specialty != "" && !containsStr(j.Specialties, specialty) {
			continue
		}
		matched = append(matched, j)
	}

	total := len(matched)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vacancies": matched[start:end],
		"total":     total,
		"page":      page,
		"pageSize":  pageSize,
	}, 0)
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// vacancyInput is the manual "Yangi vakansiya" form.
type vacancyInput struct {
	Title            string   `json:"title"`
	Company          string   `json:"company"`
	Location         string   `json:"location"`
	ApplicationEmail string   `json:"applicationEmail"`
	ContactPerson    string   `json:"contactPerson"`
	Salutation       string   `json:"salutation"`
	URL              string   `json:"url"`
	Description      string   `json:"description"`
	MedicalSpecialty string   `json:"medicalSpecialty"`
	Specialties      []string `json:"specialties"`
}

func (a *CRMAPI) createVacancy(w http.ResponseWriter, r *http.Request) {
	if a.idx == nil {
		writeError(w, http.StatusServiceUnavailable, "job index not available")
		return
	}
	var in vacancyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == "" || in.Company == "" {
		writeError(w, http.StatusBadRequest, "title and company are required")
		return
	}
	job, err := a.idx.AddManualJob(model.Job{
		Title:            in.Title,
		Company:          in.Company,
		Location:         in.Location,
		ApplicationEmail: in.ApplicationEmail,
		ContactPerson:    in.ContactPerson,
		Salutation:       in.Salutation,
		URL:              in.URL,
		Description:      in.Description,
		MedicalSpecialty: in.MedicalSpecialty,
		Specialties:      in.Specialties,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job, 0)
}

type specialtiesInput struct {
	Specialties []string `json:"specialties"`
}

func (a *CRMAPI) setVacancySpecialties(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	if _, ok := a.idx.JobByID(jobID); !ok {
		writeError(w, http.StatusNotFound, "vacancy not found")
		return
	}
	var in specialtiesInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := a.store.SetVacancyOverride(jobID, in.Specialties); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobId": jobID, "specialties": in.Specialties}, 0)
}

func (a *CRMAPI) vacancyMatches(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	job, ok := a.idx.JobByID(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "vacancy not found")
		return
	}
	job = a.withOverride(job)

	candidates, err := a.store.ListCandidates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	applications, err := a.store.ListApplications()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Score against the REAL candidate records — MatchVacancy's GmailReady
	// check needs the actual refresh token to know a connection is usable.
	// Redacting first (as this used to do) always zeroed it out, so every
	// candidate looked disconnected regardless of the real state. Only the
	// candidates embedded in the response get redacted, after scoring.
	matches := crm.MatchVacancy(candidates, applications, job)
	for i := range matches {
		matches[i].Candidate = matches[i].Candidate.Redacted()
	}
	writeJSON(w, http.StatusOK, map[string]any{"vacancy": job, "matches": matches}, 0)
}
