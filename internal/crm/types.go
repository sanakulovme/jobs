// Package crm implements the candidate/vacancy CRM layer on top of the job
// board: candidates, their documents and application profiles, sent
// applications, and employer replies. It depends on
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

// Direction is the one recruiting vertical a candidate belongs to. Exactly
// one per candidate — a higher-level classification than Specialties (which
// tags MFA vacancies/profiles within the mfa_zfa direction and stays
// unrelated to this). Only DirectionMFAZFA has a working scraper today; the
// other three exist so the CRM can show them as "tez orada" (coming soon)
// without a source site wired up yet.
const (
	DirectionMFAZFA     = "mfa_zfa"
	DirectionAusbildung = "ausbildung"
	DirectionSprachkurs = "til_kursi"
	DirectionAuPair     = "au_pair"
)

// Directions lists every valid Candidate.Direction value, in display order.
var Directions = []string{DirectionMFAZFA, DirectionAusbildung, DirectionSprachkurs, DirectionAuPair}

// IsDirection reports whether d is one of the known Directions values.
func IsDirection(d string) bool {
	for _, v := range Directions {
		if v == d {
			return true
		}
	}
	return false
}

// GmailMailbox is one Gmail account connected to a candidate. A candidate
// may have up to MaxMailboxesPerCandidate connected at once — sending
// rotates across whichever ones still have room under their own DailyCap,
// so one mailbox hitting Gmail's own sending limits doesn't stall a
// candidate's applications. Token fields must round-trip through storage
// (real json tags) but are stripped by Candidate.Redacted() before ever
// reaching the frontend.
type GmailMailbox struct {
	Slot        string    `json:"slot"` // "1".."4", stable once assigned
	Email       string    `json:"email,omitempty"`
	ConnectedAt time.Time `json:"connectedAt,omitempty"`

	AccessToken    string    `json:"accessToken,omitempty"`
	RefreshToken   string    `json:"refreshToken,omitempty"`
	Scope          string    `json:"scope,omitempty"`
	TokenExpiresAt time.Time `json:"tokenExpiresAt,omitempty"`

	// DailyCap is the admin-set max sends/day for this mailbox (1..250,
	// see MaxDailyCapPerMailbox — a self-imposed ceiling well under Gmail's
	// own ~500/day limit for regular accounts). SentToday counts sends on
	// SentTodayDate (YYYY-MM-DD, candidate/server local date); a date
	// mismatch means the counter is stale and reads as 0 until next reset.
	DailyCap      int    `json:"dailyCap"`
	SentToday     int    `json:"sentToday"`
	SentTodayDate string `json:"sentTodayDate,omitempty"`
}

// MaxMailboxesPerCandidate caps how many Gmail accounts one candidate may
// connect at once.
const MaxMailboxesPerCandidate = 4

// MaxDailyCapPerMailbox is the hard ceiling an admin can set for one
// mailbox's DailyCap — a self-imposed safety margin under Gmail's own daily
// sending limits, not a Google-enforced number.
const MaxDailyCapPerMailbox = 250

// Connected reports whether this mailbox has completed OAuth (has a refresh
// token to work with), independent of whether the access token has expired.
func (m GmailMailbox) Connected() bool { return m.Email != "" && m.RefreshToken != "" }

// sentTodayCount returns m.SentToday, or 0 if its counter is for a previous
// day (today is passed in as YYYY-MM-DD so callers share one clock read).
func (m GmailMailbox) sentTodayCount(today string) int {
	if m.SentTodayDate != today {
		return 0
	}
	return m.SentToday
}

// remainingCapacity returns how many more sends this mailbox can make today.
func (m GmailMailbox) remainingCapacity(today string) int {
	dailyCap := m.DailyCap
	if dailyCap <= 0 {
		dailyCap = MaxDailyCapPerMailbox
	}
	remaining := dailyCap - m.sentTodayCount(today)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Candidate is one person the firm is placing.
type Candidate struct {
	ID             string `json:"id"`
	FullName       string `json:"fullName"`
	ContactEmail   string `json:"contactEmail,omitempty"`
	Phone          string `json:"phone,omitempty"`
	GermanLevel    string `json:"germanLevel,omitempty"` // "" | A1..C2
	Citizenship    string `json:"citizenship,omitempty"`
	CurrentCountry string `json:"currentCountry,omitempty"`
	Notes          string `json:"notes,omitempty"`

	// Direction is one of the Directions consts — set at creation, required.
	Direction string `json:"direction"`

	GmailMailboxes []GmailMailbox `json:"gmailMailboxes,omitempty"`

	Documents []Document           `json:"documents,omitempty"`
	Profiles  []ApplicationProfile `json:"profiles,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HasCapacity reports whether this candidate has at least one connected
// mailbox with remaining daily send capacity today.
func (c Candidate) HasCapacity() bool {
	_, ok := c.NextMailbox()
	return ok
}

// NextMailbox picks which connected mailbox the next send should go
// through: the connected mailbox with remaining capacity today that was
// connected longest ago (simple round-robin — spreads load evenly across
// slots rather than hammering slot 1 until it's exhausted every day).
// Returns (nil-ish zero value, false) if no mailbox has room.
func (c Candidate) NextMailbox() (GmailMailbox, bool) {
	today := time.Now().UTC().Format("2006-01-02")
	var best GmailMailbox
	found := false
	for _, m := range c.GmailMailboxes {
		if !m.Connected() || m.remainingCapacity(today) <= 0 {
			continue
		}
		if !found || m.ConnectedAt.Before(best.ConnectedAt) {
			best = m
			found = true
		}
	}
	return best, found
}

// Mailbox looks up one of this candidate's mailboxes by slot.
func (c Candidate) Mailbox(slot string) (GmailMailbox, bool) {
	for _, m := range c.GmailMailboxes {
		if m.Slot == slot {
			return m, true
		}
	}
	return GmailMailbox{}, false
}

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
	if len(c.GmailMailboxes) > 0 {
		boxes := make([]GmailMailbox, len(c.GmailMailboxes))
		for i, m := range c.GmailMailboxes {
			m.AccessToken = ""
			m.RefreshToken = ""
			m.Scope = ""
			m.TokenExpiresAt = time.Time{}
			boxes[i] = m
		}
		c.GmailMailboxes = boxes
	}
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
	Slot        string    `json:"slot"` // which of the candidate's up-to-4 mailbox slots this link finalizes
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
