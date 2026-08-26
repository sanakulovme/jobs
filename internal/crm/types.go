// Package crm implements the candidate/vacancy CRM layer on top of the job
// board: candidates, their documents and application profiles, letter
// templates, sent applications, and employer replies. It depends on
// internal/model (for model.Job and the shared specialty vocabulary) but
// internal/model never depends back on it — the job board works standalone;
// the CRM is what's layered on top for auto-apply.
package crm

import (
	"time"

	"faangjobs/internal/model"
)

// Specialties returns the fixed specialty-slug vocabulary shared by vacancies
// (model.Job.Specialties) and candidate application profiles. It's the same
// list as model.SpecialtyVocabulary, re-exported here so callers that only
// deal with the CRM don't need to import internal/model directly.
func Specialties() []string { return model.SpecialtyVocabulary }

// Document is one uploaded file in a candidate's document library (a CV,
// cover letter, motivation letter, certificate, or other attachment), stored
// once and reusable across multiple application profiles.
type Document struct {
	ID               string `json:"id"`
	CandidateID      string `json:"candidateId"`
	Type             string `json:"type"` // "cv" | "cover_letter" | "motivation_letter" | "certificate" | "other"
	Name             string `json:"name,omitempty"`
	Language         string `json:"language,omitempty"`
	Keywords         string `json:"keywords,omitempty"`
	IsPrimaryCV      bool   `json:"isPrimaryCv"`
	OriginalFilename string `json:"originalFilename"`
	// StoredPath is relative to the CRM data dir. It must round-trip through
	// storage (hence a real json tag, not "-") but is stripped from every
	// HTTP response by Candidate.Redacted() before it reaches the frontend.
	StoredPath  string    `json:"storedPath,omitempty"`
	ContentType string    `json:"contentType,omitempty"`
	SizeBytes   int64     `json:"sizeBytes"`
	UploadedAt  time.Time `json:"uploadedAt"`
}

// ProfileSpecialty is one specialty an application profile is tagged with,
// with optional years of experience (used as a small scoring bonus).
type ProfileSpecialty struct {
	Specialty       string `json:"specialty"`
	ExperienceYears int    `json:"experienceYears,omitempty"`
}

// ApplicationProfile bundles the documents and specialties used to apply to a
// vacancy: which CV/cover letter/motivation letter to attach, and which
// specialties (from model.SpecialtyVocabulary) it's suited for.
type ApplicationProfile struct {
	ID                         string             `json:"id"`
	CandidateID                string             `json:"candidateId"`
	Name                       string             `json:"name"`
	CVDocumentID               string             `json:"cvDocumentId,omitempty"`
	CoverLetterDocumentID      string             `json:"coverLetterDocumentId,omitempty"`
	MotivationLetterDocumentID string             `json:"motivationLetterDocumentId,omitempty"`
	Specialties                []ProfileSpecialty `json:"specialties"`
	CreatedAt                  time.Time          `json:"createdAt"`
	UpdatedAt                  time.Time          `json:"updatedAt"`
}

// Candidate is one person the firm is placing. Gmail token fields are set by
// internal/gmail and persist to disk like everything else here, but must
// never leave the server as-is — always send Redacted() to the frontend.
type Candidate struct {
	ID             string `json:"id"`
	FullName       string `json:"fullName"`
	ContactEmail   string `json:"contactEmail,omitempty"`
	Phone          string `json:"phone,omitempty"`
	GermanLevel    string `json:"germanLevel,omitempty"` // "" | A1..C2
	Citizenship    string `json:"citizenship,omitempty"`
	CurrentCountry string `json:"currentCountry,omitempty"`
	Notes          string `json:"notes,omitempty"`

	GmailEmail       string    `json:"gmailEmail,omitempty"`
	GmailConnectedAt time.Time `json:"gmailConnectedAt,omitempty"`
	// The four fields below must round-trip through storage (real json tags),
	// but Redacted() strips them before a Candidate ever reaches an HTTP
	// response — see the comment there.
	GmailAccessToken    string    `json:"gmailAccessToken,omitempty"`
	GmailRefreshToken   string    `json:"gmailRefreshToken,omitempty"`
	GmailTokenExpiresAt time.Time `json:"gmailTokenExpiresAt,omitempty"`
	GmailScope          string    `json:"gmailScope,omitempty"`

	Documents []Document           `json:"documents,omitempty"`
	Profiles  []ApplicationProfile `json:"profiles,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GmailConnected reports whether this candidate has a usable Gmail
// connection (a refresh token to work with), independent of whether the
// current access token has expired.
func (c Candidate) GmailConnected() bool { return c.GmailEmail != "" && c.GmailRefreshToken != "" }

// Document looks up one of this candidate's documents by id.
func (c Candidate) Document(id string) (Document, bool) {
	for _, d := range c.Documents {
		if d.ID == id {
			return d, true
		}
	}
	return Document{}, false
}

// Redacted returns a copy of c with Gmail tokens and document storage paths
// cleared — every HTTP handler that returns a Candidate to the frontend must
// go through this first. These fields still round-trip through the on-disk
// JSON store (that's why they're not simply json:"-"); Redacted is what
// draws the line between "persisted" and "ever leaves the server."
func (c Candidate) Redacted() Candidate {
	c.GmailAccessToken = ""
	c.GmailRefreshToken = ""
	c.GmailScope = ""
	c.GmailTokenExpiresAt = time.Time{}
	if len(c.Documents) > 0 {
		docs := make([]Document, len(c.Documents))
		for i, d := range c.Documents {
			d.StoredPath = ""
			docs[i] = d
		}
		c.Documents = docs
	}
	return c
}

// LetterTemplate is a reusable application-letter body with {{token}}
// placeholders (see render.go for the supported tokens). A template may be
// narrowed to one specialty (preferred when a vacancy matches it) or left
// generic (fits any vacancy).
type LetterTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Specialty string    `json:"specialty,omitempty"` // "" = fits any vacancy
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Application statuses.
const (
	AppStatusDraft  = "draft"
	AppStatusSent   = "sent"
	AppStatusFailed = "failed"
)

// Application is one candidate's application to one vacancy (model.Job.ID).
// Recipient/sender addresses and attachment ids are snapshotted at creation
// time so a later edit to the candidate or vacancy can't rewrite history.
type Application struct {
	ID                   string `json:"id"`
	VacancyID            string `json:"vacancyId"` // model.Job.ID
	CandidateID          string `json:"candidateId"`
	CandidateName        string `json:"candidateName"`
	VacancyTitle         string `json:"vacancyTitle"`
	Employer             string `json:"employer"`
	ApplicationProfileID string `json:"applicationProfileId,omitempty"`
	LetterTemplateID     string `json:"letterTemplateId,omitempty"`

	Status  string `json:"status"`
	Subject string `json:"subject"`
	Body    string `json:"body"`

	ToEmail     string   `json:"toEmail,omitempty"`
	FromEmail   string   `json:"fromEmail,omitempty"`
	DocumentIDs []string `json:"documentIds,omitempty"`

	AutoSent bool `json:"autoSent"`

	GmailMessageID   string    `json:"gmailMessageId,omitempty"`
	GmailThreadID    string    `json:"gmailThreadId,omitempty"`
	RepliesCheckedAt time.Time `json:"repliesCheckedAt,omitempty"`

	SentAt    time.Time `json:"sentAt,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Reply categories (a "traffic light" the dashboard reports on).
const (
	ReplyGreen   = "green"
	ReplyYellow  = "yellow"
	ReplyRed     = "red"
	ReplyUnknown = "unknown"
)

// Reply is one employer reply to a sent Application, classified into a
// traffic-light category with a one-line summary.
type Reply struct {
	ID             string    `json:"id"`
	ApplicationID  string    `json:"applicationId"`
	GmailMessageID string    `json:"gmailMessageId"`
	FromEmail      string    `json:"fromEmail,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Body           string    `json:"body,omitempty"`
	Category       string    `json:"category"`
	Summary        string    `json:"summary,omitempty"`
	ReceivedAt     time.Time `json:"receivedAt"`
}

// GmailConnectToken is a one-time, shareable link that lets a candidate
// connect their own Gmail without ever logging into the admin CRM — the
// admin generates one, sends it to the candidate (Telegram, WhatsApp, ...),
// and the candidate's own click drives the OAuth consent screen so the
// grant is theirs, not the admin's.
type GmailConnectToken struct {
	Token       string    `json:"token"`
	CandidateID string    `json:"candidateId"`
	ExpiresAt   time.Time `json:"expiresAt"`
	UsedAt      time.Time `json:"usedAt,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ImportRun records one manual or scheduled scrape/send pipeline run, shown
// in the Analytics "last run" panel.
type ImportRun struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"` // "sources" | "spreadsheet"
	FileName   string         `json:"fileName,omitempty"`
	Status     string         `json:"status"` // "pending" | "running" | "done" | "failed"
	StartedAt  time.Time      `json:"startedAt,omitempty"`
	FinishedAt time.Time      `json:"finishedAt,omitempty"`
	Stats      map[string]any `json:"stats,omitempty"`
	Error      string         `json:"error,omitempty"`
}
