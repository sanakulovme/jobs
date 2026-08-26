package crm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store persists every CRM entity as one JSON file per collection under
// dataDir/crm, reusing store.WriteFileAtomic for crash-safe writes — the same
// convention the job board's own internal/store already uses for ./data.
type Store struct {
	dir        string
	uploadsDir string

	ids *counters

	candidates       *table[Candidate]
	templates        *table[LetterTemplate]
	applications     *table[Application]
	replies          *table[Reply]
	importRuns       *table[ImportRun]
	vacancyOverrides *table[vacancyOverride]
	gmailTokens      *table[GmailConnectToken]
}

// vacancyOverride is a CRM-set Specialties correction for a crawled vacancy,
// stored separately because a crawl rewrites companies/*.json wholesale and
// would otherwise silently drop the edit.
type vacancyOverride struct {
	JobID       string   `json:"jobId"`
	Specialties []string `json:"specialties"`
}

// New opens (creating if necessary) a CRM store rooted at dataDir/crm.
func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "crm")
	uploadsDir := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create crm data dir: %w", err)
	}
	return &Store{
		dir:              dir,
		uploadsDir:       uploadsDir,
		ids:              newCounters(filepath.Join(dir, "counters.json")),
		candidates:       newTable[Candidate](filepath.Join(dir, "candidates.json")),
		templates:        newTable[LetterTemplate](filepath.Join(dir, "templates.json")),
		applications:     newTable[Application](filepath.Join(dir, "applications.json")),
		replies:          newTable[Reply](filepath.Join(dir, "replies.json")),
		importRuns:       newTable[ImportRun](filepath.Join(dir, "import_runs.json")),
		vacancyOverrides: newTable[vacancyOverride](filepath.Join(dir, "vacancy_overrides.json")),
		gmailTokens:      newTable[GmailConnectToken](filepath.Join(dir, "gmail_connect_tokens.json")),
	}, nil
}

// UploadsDir is where candidate document files are stored on disk.
func (s *Store) UploadsDir() string { return s.uploadsDir }

var errNotFound = fmt.Errorf("not found")

// ErrNotFound is returned by Get/Update/Delete methods when the id doesn't
// exist.
func ErrNotFound() error { return errNotFound }

// --- candidates ---

func (s *Store) ListCandidates() ([]Candidate, error) { return s.candidates.Load() }

func (s *Store) GetCandidate(id string) (Candidate, error) {
	items, err := s.candidates.Load()
	if err != nil {
		return Candidate{}, err
	}
	for _, c := range items {
		if c.ID == id {
			return c, nil
		}
	}
	return Candidate{}, errNotFound
}

// CreateCandidate saves a new candidate (basic info step of the wizard).
func (s *Store) CreateCandidate(c Candidate) (Candidate, error) {
	id, err := s.ids.Next("candidates")
	if err != nil {
		return Candidate{}, err
	}
	now := time.Now().UTC()
	c.ID, c.CreatedAt, c.UpdatedAt = id, now, now
	err = s.candidates.Update(func(items []Candidate) ([]Candidate, error) {
		return append(items, c), nil
	})
	return c, err
}

// UpdateCandidate applies fn to the candidate matching id and saves the
// result; fn returns the mutated candidate. Used by every wizard step
// (basic info / gmail / profiles) so each can save independently.
func (s *Store) UpdateCandidate(id string, fn func(Candidate) (Candidate, error)) (Candidate, error) {
	var updated Candidate
	err := s.candidates.Update(func(items []Candidate) ([]Candidate, error) {
		for i := range items {
			if items[i].ID != id {
				continue
			}
			next, err := fn(items[i])
			if err != nil {
				return nil, err
			}
			next.ID = id
			next.UpdatedAt = time.Now().UTC()
			items[i] = next
			updated = next
			return items, nil
		}
		return nil, errNotFound
	})
	return updated, err
}

func (s *Store) DeleteCandidate(id string) error {
	return s.candidates.Update(func(items []Candidate) ([]Candidate, error) {
		out := items[:0]
		found := false
		for _, c := range items {
			if c.ID == id {
				found = true
				continue
			}
			out = append(out, c)
		}
		if !found {
			return nil, errNotFound
		}
		return out, nil
	})
}

// --- documents (nested under a candidate) ---

// AddDocument appends a document to a candidate's library.
func (s *Store) AddDocument(candidateID string, d Document) (Document, error) {
	id, err := s.ids.Next("documents")
	if err != nil {
		return Document{}, err
	}
	d.ID, d.CandidateID, d.UploadedAt = id, candidateID, time.Now().UTC()
	_, err = s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		if d.IsPrimaryCV {
			for i := range c.Documents {
				c.Documents[i].IsPrimaryCV = false
			}
		}
		c.Documents = append(c.Documents, d)
		return c, nil
	})
	return d, err
}

func (s *Store) DeleteDocument(candidateID, docID string) error {
	_, err := s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		out := c.Documents[:0]
		found := false
		for _, d := range c.Documents {
			if d.ID == docID {
				found = true
				continue
			}
			out = append(out, d)
		}
		if !found {
			return c, errNotFound
		}
		c.Documents = out
		return c, nil
	})
	return err
}

// --- application profiles (nested under a candidate) ---

func (s *Store) AddProfile(candidateID string, p ApplicationProfile) (ApplicationProfile, error) {
	id, err := s.ids.Next("profiles")
	if err != nil {
		return ApplicationProfile{}, err
	}
	now := time.Now().UTC()
	p.ID, p.CandidateID, p.CreatedAt, p.UpdatedAt = id, candidateID, now, now
	_, err = s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		c.Profiles = append(c.Profiles, p)
		return c, nil
	})
	return p, err
}

func (s *Store) DeleteProfile(candidateID, profileID string) error {
	_, err := s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		out := c.Profiles[:0]
		found := false
		for _, p := range c.Profiles {
			if p.ID == profileID {
				found = true
				continue
			}
			out = append(out, p)
		}
		if !found {
			return c, errNotFound
		}
		c.Profiles = out
		return c, nil
	})
	return err
}

// --- letter templates ---

func (s *Store) ListTemplates() ([]LetterTemplate, error) { return s.templates.Load() }

func (s *Store) CreateTemplate(t LetterTemplate) (LetterTemplate, error) {
	id, err := s.ids.Next("templates")
	if err != nil {
		return LetterTemplate{}, err
	}
	now := time.Now().UTC()
	t.ID, t.CreatedAt, t.UpdatedAt = id, now, now
	err = s.templates.Update(func(items []LetterTemplate) ([]LetterTemplate, error) {
		if t.IsDefault {
			for i := range items {
				items[i].IsDefault = false
			}
		}
		return append(items, t), nil
	})
	return t, err
}

func (s *Store) UpdateTemplate(id string, fn func(LetterTemplate) (LetterTemplate, error)) (LetterTemplate, error) {
	var updated LetterTemplate
	err := s.templates.Update(func(items []LetterTemplate) ([]LetterTemplate, error) {
		for i := range items {
			if items[i].ID != id {
				continue
			}
			next, err := fn(items[i])
			if err != nil {
				return nil, err
			}
			next.ID = id
			next.UpdatedAt = time.Now().UTC()
			if next.IsDefault {
				for j := range items {
					items[j].IsDefault = false
				}
			}
			items[i] = next
			updated = next
			return items, nil
		}
		return nil, errNotFound
	})
	return updated, err
}

func (s *Store) DeleteTemplate(id string) error {
	return s.templates.Update(func(items []LetterTemplate) ([]LetterTemplate, error) {
		out := items[:0]
		found := false
		for _, t := range items {
			if t.ID == id {
				found = true
				continue
			}
			out = append(out, t)
		}
		if !found {
			return nil, errNotFound
		}
		return out, nil
	})
}

// --- applications ---

func (s *Store) ListApplications() ([]Application, error) { return s.applications.Load() }

// errDuplicateApplication guards the same rule the reference app enforces
// with a DB unique constraint: one application per (candidate, vacancy) pair.
var errDuplicateApplication = fmt.Errorf("application already exists for this candidate and vacancy")

// ErrDuplicateApplication is returned by CreateApplication when the pair
// already has a record.
func ErrDuplicateApplication() error { return errDuplicateApplication }

func (s *Store) CreateApplication(a Application) (Application, error) {
	id, err := s.ids.Next("applications")
	if err != nil {
		return Application{}, err
	}
	a.ID, a.CreatedAt = id, time.Now().UTC()
	if a.Status == "" {
		a.Status = AppStatusDraft
	}
	err = s.applications.Update(func(items []Application) ([]Application, error) {
		for _, existing := range items {
			if existing.CandidateID == a.CandidateID && existing.VacancyID == a.VacancyID && existing.Status != AppStatusFailed {
				return nil, errDuplicateApplication
			}
		}
		return append(items, a), nil
	})
	return a, err
}

func (s *Store) UpdateApplication(id string, fn func(Application) (Application, error)) (Application, error) {
	var updated Application
	err := s.applications.Update(func(items []Application) ([]Application, error) {
		for i := range items {
			if items[i].ID != id {
				continue
			}
			next, err := fn(items[i])
			if err != nil {
				return nil, err
			}
			next.ID = id
			items[i] = next
			updated = next
			return items, nil
		}
		return nil, errNotFound
	})
	return updated, err
}

// --- replies ---

func (s *Store) ListReplies() ([]Reply, error) { return s.replies.Load() }

// AddReply records a reply, deduped by (applicationID, gmailMessageID) so
// repeated polling can't duplicate one. Returns (reply, true) if newly
// recorded, (zero, false) if it was already on file.
func (s *Store) AddReply(r Reply) (Reply, bool, error) {
	id, err := s.ids.Next("replies")
	if err != nil {
		return Reply{}, false, err
	}
	added := false
	err = s.replies.Update(func(items []Reply) ([]Reply, error) {
		for _, existing := range items {
			if existing.ApplicationID == r.ApplicationID && existing.GmailMessageID == r.GmailMessageID {
				return items, nil
			}
		}
		r.ID = id
		items = append(items, r)
		added = true
		return items, nil
	})
	if !added {
		return Reply{}, false, err
	}
	return r, true, err
}

// --- import runs ---

func (s *Store) CreateImportRun(r ImportRun) (ImportRun, error) {
	id, err := s.ids.Next("import_runs")
	if err != nil {
		return ImportRun{}, err
	}
	r.ID = id
	err = s.importRuns.Update(func(items []ImportRun) ([]ImportRun, error) {
		return append(items, r), nil
	})
	return r, err
}

func (s *Store) UpdateImportRun(id string, fn func(ImportRun) (ImportRun, error)) (ImportRun, error) {
	var updated ImportRun
	err := s.importRuns.Update(func(items []ImportRun) ([]ImportRun, error) {
		for i := range items {
			if items[i].ID != id {
				continue
			}
			next, err := fn(items[i])
			if err != nil {
				return nil, err
			}
			next.ID = id
			items[i] = next
			updated = next
			return items, nil
		}
		return nil, errNotFound
	})
	return updated, err
}

// LatestImportRun returns the most recently created run, if any.
func (s *Store) LatestImportRun() (ImportRun, bool, error) {
	items, err := s.importRuns.Load()
	if err != nil || len(items) == 0 {
		return ImportRun{}, false, err
	}
	return items[len(items)-1], true, nil
}

// --- vacancy specialty overrides ---

// VacancyOverride returns the CRM-set Specialties for jobID, if any operator
// correction has been recorded.
func (s *Store) VacancyOverride(jobID string) ([]string, bool, error) {
	items, err := s.vacancyOverrides.Load()
	if err != nil {
		return nil, false, err
	}
	for _, v := range items {
		if v.JobID == jobID {
			return v.Specialties, true, nil
		}
	}
	return nil, false, nil
}

// SetVacancyOverride records (or replaces) the Specialties override for jobID.
func (s *Store) SetVacancyOverride(jobID string, specialties []string) error {
	return s.vacancyOverrides.Update(func(items []vacancyOverride) ([]vacancyOverride, error) {
		for i := range items {
			if items[i].JobID == jobID {
				items[i].Specialties = specialties
				return items, nil
			}
		}
		return append(items, vacancyOverride{JobID: jobID, Specialties: specialties}), nil
	})
}

// --- Gmail connect tokens (one-time shareable links) ---

// gmailTokenBytes is the random-token length in bytes (32 hex chars once
// encoded) — long enough that guessing one is infeasible, short enough to
// fit comfortably in a URL a candidate is asked to click.
const gmailTokenBytes = 16

// gmailTokenTTL is how long a generated connect link stays valid before the
// admin has to issue a fresh one.
const gmailTokenTTL = 48 * time.Hour

// CreateGmailConnectToken mints a fresh one-time token for candidateID.
func (s *Store) CreateGmailConnectToken(candidateID string) (GmailConnectToken, error) {
	raw := make([]byte, gmailTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return GmailConnectToken{}, fmt.Errorf("generate token: %w", err)
	}
	now := time.Now().UTC()
	t := GmailConnectToken{
		Token:       hex.EncodeToString(raw),
		CandidateID: candidateID,
		ExpiresAt:   now.Add(gmailTokenTTL),
		CreatedAt:   now,
	}
	err := s.gmailTokens.Update(func(items []GmailConnectToken) ([]GmailConnectToken, error) {
		return append(items, t), nil
	})
	return t, err
}

// ResolveGmailConnectToken returns the token record if it exists, is unused,
// and hasn't expired — the three conditions the public connect link and
// callback both have to check before trusting it.
func (s *Store) ResolveGmailConnectToken(token string) (GmailConnectToken, error) {
	items, err := s.gmailTokens.Load()
	if err != nil {
		return GmailConnectToken{}, err
	}
	for _, t := range items {
		if t.Token != token {
			continue
		}
		if !t.UsedAt.IsZero() {
			return GmailConnectToken{}, fmt.Errorf("ushbu havola allaqachon ishlatilgan")
		}
		if time.Now().UTC().After(t.ExpiresAt) {
			return GmailConnectToken{}, fmt.Errorf("havola muddati tugagan")
		}
		return t, nil
	}
	return GmailConnectToken{}, errNotFound
}

// MarkGmailConnectTokenUsed consumes a token so it can't be replayed.
func (s *Store) MarkGmailConnectTokenUsed(token string) error {
	return s.gmailTokens.Update(func(items []GmailConnectToken) ([]GmailConnectToken, error) {
		for i := range items {
			if items[i].Token == token {
				items[i].UsedAt = time.Now().UTC()
				return items, nil
			}
		}
		return items, nil
	})
}

// SetCandidateGmail records a successful Gmail connection on a candidate.
func (s *Store) SetCandidateGmail(candidateID, email, accessToken, refreshToken, scope string, expiresAt time.Time) (Candidate, error) {
	return s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		c.GmailEmail = email
		c.GmailConnectedAt = time.Now().UTC()
		c.GmailAccessToken = accessToken
		c.GmailRefreshToken = refreshToken
		c.GmailTokenExpiresAt = expiresAt
		c.GmailScope = scope
		return c, nil
	})
}

// ClearCandidateGmail disconnects a candidate's Gmail (the "Uzish" button).
func (s *Store) ClearCandidateGmail(candidateID string) (Candidate, error) {
	return s.UpdateCandidate(candidateID, func(c Candidate) (Candidate, error) {
		c.GmailEmail = ""
		c.GmailConnectedAt = time.Time{}
		c.GmailAccessToken = ""
		c.GmailRefreshToken = ""
		c.GmailTokenExpiresAt = time.Time{}
		c.GmailScope = ""
		return c, nil
	})
}
