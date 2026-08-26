package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"faangjobs/internal/crm"
	"faangjobs/internal/gmail"
	"faangjobs/internal/model"
)

// Auto-apply gates, ported from the reference app's AUTO_APPLY_* env vars
// (see .env.example there: MAX_PER_RUN=20, MAX_PER_CANDIDATE=5, MIN_SCORE=90,
// MIN_GERMAN_LEVEL=B1). Hardcoded for now; making these operator-configurable
// is a natural follow-up now that phase 6 (real sending) makes the stakes of
// getting them wrong actually matter.
const (
	defaultMaxPerRun       = 20
	defaultMaxPerCandidate = 5
	defaultMinScore        = 90
	defaultMinGermanLevel  = "B1"
)

func (a *CRMAPI) registerRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/crm/run", a.runPipeline)
	mux.HandleFunc("GET /api/crm/run", a.lastRun)
}

type runInput struct {
	Count    int  `json:"count"`
	TestMode bool `json:"testMode"`
}

// RunResult is the outcome of one RunAutoApply pass.
type RunResult struct {
	Run    crm.ImportRun
	Stats  crm.AutoApplyStats
	Drafts []crm.Application
}

// errGmailNotConfigured is returned by RunAutoApply when a real (non-dry-run)
// pass is requested but no Gmail OAuth credentials are configured — callers
// (the HTTP handler, the standalone CLI) turn this into their own "can't do
// that" response rather than silently falling back to a dry run.
var errGmailNotConfigured = fmt.Errorf("Gmail integratsiyasi sozlanmagan — hozircha faqat sinov rejimida (testMode: true) ishlaydi")

// runPipeline is the thin HTTP wrapper around RunAutoApply — see that method
// for what an auto-apply pass actually does.
func (a *CRMAPI) runPipeline(w http.ResponseWriter, r *http.Request) {
	if a.idx == nil {
		writeError(w, http.StatusServiceUnavailable, "job index not available")
		return
	}
	var in runInput
	if !decodeJSON(w, r, &in) {
		return
	}

	result, err := a.RunAutoApply(in.Count, in.TestMode)
	if err != nil {
		if errors.Is(err, errGmailNotConfigured) {
			writeError(w, http.StatusNotImplemented, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"runId":  result.Run.ID,
		"stats":  result.Stats,
		"drafts": result.Drafts,
		"dryRun": in.TestMode,
	}, 0)
}

// RunAutoApply matches every currently-indexed vacancy against every
// candidate and either reports what an auto-apply run would do (testMode)
// or actually sends the qualifying letters through each candidate's
// connected Gmail (testMode:false, rejected with errGmailNotConfigured if
// Gmail isn't configured at all, so a caller can't mistake "not wired up"
// for "ran and sent nothing"). count<=0 uses defaultMaxPerRun.
//
// This has no HTTP dependency, so it's callable both from the /api/crm/run
// handler above and from a standalone process (see cmd/autoapply) that
// chains a real run after the daily crawl without needing an HTTP round
// trip through the site's 42.uz auth — see scripts/daily-crawl.sh for how
// the two are meant to be wired together on the production server.
func (a *CRMAPI) RunAutoApply(count int, testMode bool) (RunResult, error) {
	if a.idx == nil {
		return RunResult{}, fmt.Errorf("job index not available")
	}
	if !testMode && !a.gmail.Enabled() {
		return RunResult{}, errGmailNotConfigured
	}

	maxPerRun := defaultMaxPerRun
	if count > 0 {
		maxPerRun = count
	}

	candidates, err := a.store.ListCandidates()
	if err != nil {
		return RunResult{}, err
	}
	applications, err := a.store.ListApplications()
	if err != nil {
		return RunResult{}, err
	}
	templates, err := a.store.ListTemplates()
	if err != nil {
		return RunResult{}, err
	}

	snap := a.idx.Snapshot()
	vacancies := make([]model.Job, len(snap.jobs))
	for i, j := range snap.jobs {
		vacancies[i] = a.withOverride(j)
	}

	started := time.Now().UTC()
	drafts := []crm.Application{}
	var apply func(job model.Job, m crm.Match, tmpl crm.LetterTemplate) (crm.Application, error)

	if testMode {
		// Dry run: rendered and reported, never persisted — matches the
		// reference app's AutoApplyService, which returns before the DB
		// insert when $dryRun is true. Nothing here counts against the
		// duplicate-application guard until a real send actually happens.
		apply = func(job model.Job, m crm.Match, tmpl crm.LetterTemplate) (crm.Application, error) {
			subject, body, err := crm.RenderTemplate(tmpl, crm.ContextFor(job, m.Candidate))
			if err != nil {
				return crm.Application{}, err
			}
			app := crm.Application{
				VacancyID: job.ID, CandidateID: m.Candidate.ID, CandidateName: m.Candidate.FullName,
				VacancyTitle: job.Title, Employer: job.Company,
				ApplicationProfileID: m.Profile.ID, LetterTemplateID: tmpl.ID,
				Status: crm.AppStatusDraft, Subject: subject, Body: body,
				ToEmail: job.ApplicationEmail, FromEmail: m.Candidate.GmailEmail,
				DocumentIDs: profileDocumentIDs(m.Profile),
			}
			drafts = append(drafts, app)
			return app, nil
		}
	} else {
		apply = a.realApply
	}

	opts := crm.AutoApplyOptions{
		MaxPerRun:       maxPerRun,
		MaxPerCandidate: defaultMaxPerCandidate,
		MinScore:        defaultMinScore,
		MinGermanLevel:  defaultMinGermanLevel,
		DryRun:          testMode,
	}
	stats := crm.RunAutoApply(vacancies, candidates, applications, templates, opts, apply)

	run, err := a.store.CreateImportRun(crm.ImportRun{
		Type:       "sources",
		Status:     "done",
		StartedAt:  started,
		FinishedAt: time.Now().UTC(),
		Stats: map[string]any{
			"considered": stats.Considered,
			"sent":       stats.Sent,
			"skipped":    stats.Skipped,
			"failed":     stats.Failed,
			"noEmail":    stats.NoEmail,
			"testMode":   testMode,
		},
	})
	if err != nil {
		return RunResult{}, err
	}

	return RunResult{Run: run, Stats: stats, Drafts: drafts}, nil
}

// realApply renders the letter, records a draft Application (so the
// duplicate-send guard covers it even if the send itself fails), sends it
// through the candidate's Gmail, and updates the record to sent/failed.
// This is the ONLY code path in the whole CRM that talks to Gmail's send
// endpoint.
func (a *CRMAPI) realApply(job model.Job, m crm.Match, tmpl crm.LetterTemplate) (crm.Application, error) {
	ctx := context.Background()

	subject, body, err := crm.RenderTemplate(tmpl, crm.ContextFor(job, m.Candidate))
	if err != nil {
		return crm.Application{}, err
	}

	app, err := a.store.CreateApplication(crm.Application{
		VacancyID: job.ID, CandidateID: m.Candidate.ID, CandidateName: m.Candidate.FullName,
		VacancyTitle: job.Title, Employer: job.Company,
		ApplicationProfileID: m.Profile.ID, LetterTemplateID: tmpl.ID,
		Status: crm.AppStatusDraft, Subject: subject, Body: body,
		ToEmail: job.ApplicationEmail, FromEmail: m.Candidate.GmailEmail,
		DocumentIDs: profileDocumentIDs(m.Profile),
		AutoSent:    true,
	})
	if err != nil {
		return crm.Application{}, err
	}

	result, sendErr := a.sendApplication(ctx, m.Candidate, m.Profile, job.ApplicationEmail, subject, body)
	if sendErr != nil {
		_, _ = a.store.UpdateApplication(app.ID, func(rec crm.Application) (crm.Application, error) {
			rec.Status = crm.AppStatusFailed
			rec.Error = sendErr.Error()
			return rec, nil
		})
		return crm.Application{}, sendErr
	}

	updated, err := a.store.UpdateApplication(app.ID, func(rec crm.Application) (crm.Application, error) {
		rec.Status = crm.AppStatusSent
		rec.SentAt = time.Now().UTC()
		rec.GmailMessageID = result.MessageID
		rec.GmailThreadID = result.ThreadID
		return rec, nil
	})
	if err != nil {
		return crm.Application{}, err
	}
	return updated, nil
}

// sendApplication resolves a fresh access token and the profile's attached
// documents, then hands off to internal/gmail for the actual MIME
// construction and API call.
func (a *CRMAPI) sendApplication(ctx context.Context, candidate crm.Candidate, profile crm.ApplicationProfile, to, subject, body string) (gmail.SendResult, error) {
	accessToken, err := a.validAccessToken(ctx, candidate)
	if err != nil {
		return gmail.SendResult{}, err
	}
	attachments, err := a.profileAttachments(candidate, profile)
	if err != nil {
		return gmail.SendResult{}, err
	}
	tok := gmailToken(accessToken)
	return a.gmail.Send(ctx, tok, candidate.FullName, candidate.GmailEmail, to, subject, body, attachments)
}

// gmailToken builds the minimal gmail.Token Send() needs — just the access
// token, since validAccessToken already guaranteed it's fresh.
func gmailToken(accessToken string) gmail.Token { return gmail.Token{AccessToken: accessToken} }

// profileAttachments loads the CV/cover-letter/motivation-letter files a
// profile references, in that fixed order, skipping any that were removed
// from the document library since the profile was created.
func (a *CRMAPI) profileAttachments(candidate crm.Candidate, profile crm.ApplicationProfile) ([]gmail.Attachment, error) {
	var out []gmail.Attachment
	for _, docID := range []string{profile.CVDocumentID, profile.CoverLetterDocumentID, profile.MotivationLetterDocumentID} {
		if docID == "" {
			continue
		}
		doc, ok := candidate.Document(docID)
		if !ok {
			continue
		}
		f, err := a.store.OpenUpload(doc.StoredPath)
		if err != nil {
			return nil, fmt.Errorf("hujjatni ochib bo'lmadi (%s): %w", doc.OriginalFilename, err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("hujjatni o'qib bo'lmadi (%s): %w", doc.OriginalFilename, err)
		}
		out = append(out, gmail.Attachment{Filename: doc.OriginalFilename, ContentType: doc.ContentType, Data: data})
	}
	return out, nil
}

func (a *CRMAPI) lastRun(w http.ResponseWriter, r *http.Request) {
	run, ok, err := a.store.LatestImportRun()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run}, 0)
}

func profileDocumentIDs(p crm.ApplicationProfile) []string {
	ids := []string{}
	for _, id := range []string{p.CVDocumentID, p.CoverLetterDocumentID, p.MotivationLetterDocumentID} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
