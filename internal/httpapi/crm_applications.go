package httpapi

import "net/http"

func (a *CRMAPI) registerApplicationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/crm/applications", a.listApplications)
}

func (a *CRMAPI) listApplications(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListApplications()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	candidateID := r.URL.Query().Get("candidateId")
	jobID := r.URL.Query().Get("vacancyId")
	status := r.URL.Query().Get("status")

	// items[:0:0] (zero-cap) rather than `var filtered []crm.Application`: an
	// empty-but-nil slice marshals to JSON `null`, which crashed the frontend
	// once already (see crm.MatchVacancy) — always keep this non-nil.
	filtered := items[:0:0]
	for _, app := range items {
		if candidateID != "" && app.CandidateID != candidateID {
			continue
		}
		if jobID != "" && app.VacancyID != jobID {
			continue
		}
		if status != "" && app.Status != status {
			continue
		}
		filtered = append(filtered, app)
	}

	// Most recent first.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	writeJSON(w, http.StatusOK, map[string]any{"applications": filtered, "total": len(filtered)}, 0)
}
