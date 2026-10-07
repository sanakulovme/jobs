package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"faangjobs/internal/ai"
	"faangjobs/internal/crm"
	"faangjobs/internal/gmail"
	"faangjobs/internal/source"
	"faangjobs/internal/store"
)

// CRMAPI exposes the candidate/vacancy CRM JSON endpoints under /api/crm/*.
type CRMAPI struct {
	store      *crm.Store
	idx        *Index
	gmail      *gmail.Client
	classifier crm.Classifier
	// jobStore/fetcher back the on-demand scrape endpoint (crm_scrape.go);
	// both nil disables that route with a clear error.
	jobStore *store.Store
	fetcher  *source.Fetcher
	// letters writes every application e-mail; nil means no AI key is
	// configured and auto-apply runs fail with errLettersNotConfigured.
	letters LetterWriter
	// pages reads arbitrary job pages for "any site" scrapes; nil disables
	// that source with a clear error.
	pages PageJobExtractor
}

// LetterWriter writes one application e-mail for a matched candidate and
// vacancy — *ai.Groq or *ai.Claude.
type LetterWriter interface {
	Write(ctx context.Context, in ai.LetterInput) (ai.Letter, error)
}

// NewCRMAPI builds a CRM API handler set. idx may be nil until the vacancy
// routes need it; gmailClient may be nil (or built from an empty
// gmail.Config) until Gmail credentials are configured — every Gmail route
// checks gmail.Enabled() itself and responds 503 rather than panicking.
// jobStore/fetcher may be nil (the on-demand scrape route responds 503
// instead of panicking); so may letters (auto-apply runs then fail with a
// clear "AI not configured" error). Reply classification defaults to the
// dependency-free crm.KeywordClassifier; swapping in an LLM-backed one later
// is a one-line change here.
func NewCRMAPI(crmStore *crm.Store, idx *Index, gmailClient *gmail.Client, jobStore *store.Store, fetcher *source.Fetcher, letters LetterWriter, pages PageJobExtractor) *CRMAPI {
	if gmailClient == nil {
		gmailClient = gmail.New(gmail.Config{})
	}
	return &CRMAPI{
		store: crmStore, idx: idx, gmail: gmailClient, classifier: crm.KeywordClassifier{},
		jobStore: jobStore, fetcher: fetcher, letters: letters, pages: pages,
	}
}

// Register wires the CRM routes onto a mux.
func (a *CRMAPI) Register(mux *http.ServeMux) {
	if a.store == nil {
		return
	}
	mux.HandleFunc("GET /api/crm/candidates", a.listCandidates)
	mux.HandleFunc("POST /api/crm/candidates", a.createCandidate)
	mux.HandleFunc("GET /api/crm/candidates/{id}", a.getCandidate)
	mux.HandleFunc("PATCH /api/crm/candidates/{id}", a.updateCandidate)
	mux.HandleFunc("DELETE /api/crm/candidates/{id}", a.deleteCandidate)

	mux.HandleFunc("GET /api/crm/candidates/{id}/documents", a.listDocuments)
	mux.HandleFunc("POST /api/crm/candidates/{id}/documents", a.uploadDocument)
	mux.HandleFunc("GET /api/crm/candidates/{id}/documents/{docId}/file", a.downloadDocument)
	mux.HandleFunc("DELETE /api/crm/candidates/{id}/documents/{docId}", a.deleteDocument)

	mux.HandleFunc("GET /api/crm/candidates/{id}/profiles", a.listProfiles)
	mux.HandleFunc("POST /api/crm/candidates/{id}/profiles", a.createProfile)
	mux.HandleFunc("DELETE /api/crm/candidates/{id}/profiles/{profileId}", a.deleteProfile)

	mux.HandleFunc("GET /api/crm/specialties", a.listSpecialties)

	a.registerVacancyRoutes(mux)
	a.registerApplicationRoutes(mux)
	a.registerRunRoutes(mux)
	a.registerAnalyticsRoutes(mux)
	a.registerGmailRoutes(mux)
	a.registerReplyCheckRoutes(mux)
	a.registerScrapeRoutes(mux)
}

// --- candidates ---

func (a *CRMAPI) listCandidates(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListCandidates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if direction := r.URL.Query().Get("direction"); direction != "" {
		filtered := items[:0:0]
		for _, c := range items {
			if c.Direction == direction {
				filtered = append(filtered, c)
			}
		}
		items = filtered
	}
	for i := range items {
		items[i] = items[i].Redacted()
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": items, "total": len(items)}, 0)
}

type candidateInput struct {
	FullName       string `json:"fullName"`
	ContactEmail   string `json:"contactEmail"`
	Phone          string `json:"phone"`
	GermanLevel    string `json:"germanLevel"`
	Citizenship    string `json:"citizenship"`
	CurrentCountry string `json:"currentCountry"`
	Notes          string `json:"notes"`
	Direction      string `json:"direction"`
}

func (in candidateInput) apply(c crm.Candidate) crm.Candidate {
	c.FullName = in.FullName
	c.ContactEmail = in.ContactEmail
	c.Phone = in.Phone
	c.GermanLevel = in.GermanLevel
	c.Citizenship = in.Citizenship
	c.CurrentCountry = in.CurrentCountry
	c.Notes = in.Notes
	c.Direction = in.Direction
	return c
}

func (a *CRMAPI) createCandidate(w http.ResponseWriter, r *http.Request) {
	var in candidateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.FullName == "" {
		writeError(w, http.StatusBadRequest, "fullName is required")
		return
	}
	if !crm.IsDirection(in.Direction) {
		writeError(w, http.StatusBadRequest, "direction must be one of: "+strings.Join(crm.Directions, ", "))
		return
	}
	c, err := a.store.CreateCandidate(in.apply(crm.Candidate{}))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c.Redacted(), 0)
}

func (a *CRMAPI) getCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCandidate(r.PathValue("id"))
	if !writeStoreResult(w, c, err) {
		return
	}
	writeJSON(w, http.StatusOK, c.Redacted(), 0)
}

func (a *CRMAPI) updateCandidate(w http.ResponseWriter, r *http.Request) {
	var in candidateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	c, err := a.store.UpdateCandidate(r.PathValue("id"), func(existing crm.Candidate) (crm.Candidate, error) {
		return in.apply(existing), nil
	})
	if !writeStoreResult(w, c, err) {
		return
	}
	writeJSON(w, http.StatusOK, c.Redacted(), 0)
}

func (a *CRMAPI) deleteCandidate(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteCandidate(r.PathValue("id"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- documents ---

func (a *CRMAPI) listDocuments(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCandidate(r.PathValue("id"))
	if !writeStoreResult(w, c, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": c.Redacted().Documents}, 0)
}

// uploadDocument accepts multipart/form-data with fields: file (required),
// type, name, language, keywords, isPrimaryCv.
func (a *CRMAPI) uploadDocument(w http.ResponseWriter, r *http.Request) {
	candidateID := r.PathValue("id")
	if _, err := a.store.GetCandidate(candidateID); err != nil {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, crm.MaxUploadBytes+1<<20) // + headroom for form fields
	if err := r.ParseMultipartForm(crm.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	docType := r.FormValue("type")
	if docType == "" {
		docType = "other"
	}

	doc := crm.Document{
		Type:             docType,
		Name:             r.FormValue("name"),
		Language:         r.FormValue("language"),
		Keywords:         r.FormValue("keywords"),
		IsPrimaryCV:      r.FormValue("isPrimaryCv") == "true",
		OriginalFilename: filepath.Base(header.Filename),
		ContentType:      header.Header.Get("Content-Type"),
	}

	// The document needs an id before SaveUpload can name the file, but
	// AddDocument is what mints one — mint it here via a throwaway id from
	// the store's own counter path instead, so the two stay in sync: create
	// the metadata record first, then save the file under its id.
	created, err := a.store.AddDocument(candidateID, doc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	storedPath, size, err := a.store.SaveUpload(candidateID, created.ID, created.OriginalFilename, file)
	if err != nil {
		_ = a.store.DeleteDocument(candidateID, created.ID)
		writeError(w, http.StatusInternalServerError, "save upload: "+err.Error())
		return
	}
	updated, err := a.store.UpdateCandidate(candidateID, func(c crm.Candidate) (crm.Candidate, error) {
		for i := range c.Documents {
			if c.Documents[i].ID == created.ID {
				c.Documents[i].StoredPath = storedPath
				c.Documents[i].SizeBytes = size
			}
		}
		return c, nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, d := range updated.Redacted().Documents {
		if d.ID == created.ID {
			writeJSON(w, http.StatusCreated, d, 0)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "document vanished after upload")
}

func (a *CRMAPI) downloadDocument(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCandidate(r.PathValue("id"))
	if !writeStoreResult(w, c, err) {
		return
	}
	docID := r.PathValue("docId")
	for _, d := range c.Documents {
		if d.ID != docID {
			continue
		}
		f, err := a.store.OpenUpload(d.StoredPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found on disk")
			return
		}
		defer f.Close()
		if d.ContentType != "" {
			w.Header().Set("Content-Type", d.ContentType)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+d.OriginalFilename+`"`)
		_, _ = io.Copy(w, f)
		return
	}
	writeError(w, http.StatusNotFound, "document not found")
}

func (a *CRMAPI) deleteDocument(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteDocument(r.PathValue("id"), r.PathValue("docId"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- application profiles ---

func (a *CRMAPI) listProfiles(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCandidate(r.PathValue("id"))
	if !writeStoreResult(w, c, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": c.Profiles}, 0)
}

type profileInput struct {
	Name                       string                 `json:"name"`
	CVDocumentID               string                 `json:"cvDocumentId"`
	CoverLetterDocumentID      string                 `json:"coverLetterDocumentId"`
	MotivationLetterDocumentID string                 `json:"motivationLetterDocumentId"`
	Specialties                []crm.ProfileSpecialty `json:"specialties"`
}

func (a *CRMAPI) createProfile(w http.ResponseWriter, r *http.Request) {
	candidateID := r.PathValue("id")
	var in profileInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(in.Specialties) == 0 {
		writeError(w, http.StatusBadRequest, "at least one specialty is required")
		return
	}
	p, err := a.store.AddProfile(candidateID, crm.ApplicationProfile{
		Name:                       in.Name,
		CVDocumentID:               in.CVDocumentID,
		CoverLetterDocumentID:      in.CoverLetterDocumentID,
		MotivationLetterDocumentID: in.MotivationLetterDocumentID,
		Specialties:                in.Specialties,
	})
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p, 0)
}

func (a *CRMAPI) deleteProfile(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteProfile(r.PathValue("id"), r.PathValue("profileId"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- specialty vocabulary (for building the frontend's tag pickers) ---

func (a *CRMAPI) listSpecialties(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"specialties": crm.Specialties()}, time.Hour)
}

// --- shared helpers ---

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 5<<20))
	if err := dec.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// writeStoreResult writes the appropriate error response for a store
// Get/Update result and reports whether the caller should continue writing
// a success response.
func writeStoreResult[T any](w http.ResponseWriter, _ T, err error) bool {
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}
