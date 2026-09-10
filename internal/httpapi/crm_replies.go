package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"faangjobs/internal/crm"
	"faangjobs/internal/gmail"
)

func (a *CRMAPI) registerReplyCheckRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/crm/replies/check", a.checkReplies)
}

type checkRepliesInput struct {
	Limit int `json:"limit"`
}

type checkRepliesStats struct {
	Checked    int            `json:"checked"`
	NewReplies int            `json:"newReplies"`
	Failed     int            `json:"failed"`
	ByCategory map[string]int `json:"byCategory"`
}

// checkReplies polls the Gmail thread of every sent application (the
// longest-unchecked ones first, so one poll can't starve older threads) and
// records any employer reply the CRM hasn't seen yet, classifying each
// newly-recorded one with a.classifier. Mirrors the reference app's
// ReplyChecker::run() — a per-application failure (expired Gmail grant,
// unreachable Gmail) is counted in the stats and does not abort the batch.
func (a *CRMAPI) checkReplies(w http.ResponseWriter, r *http.Request) {
	if !a.gmail.Enabled() {
		writeError(w, http.StatusNotImplemented, "Gmail integratsiyasi sozlanmagan")
		return
	}
	var in checkRepliesInput
	if !decodeJSON(w, r, &in) {
		return
	}
	limit := 100
	if in.Limit > 0 {
		limit = in.Limit
	}

	applications, err := a.store.ListApplications()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	existingReplies, err := a.store.ListReplies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seen := make(map[string]bool, len(existingReplies))
	for _, rep := range existingReplies {
		seen[rep.ApplicationID+"|"+rep.GmailMessageID] = true
	}

	ctx := r.Context()
	stats := checkRepliesStats{ByCategory: map[string]int{}}
	for _, app := range pendingApplications(applications, limit) {
		stats.Checked++
		if err := a.checkApplicationReplies(ctx, app, seen, &stats); err != nil {
			stats.Failed++
			continue
		}
		if _, err := a.store.UpdateApplication(app.ID, func(rec crm.Application) (crm.Application, error) {
			rec.RepliesCheckedAt = time.Now().UTC()
			return rec, nil
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, stats, 0)
}

// pendingApplications selects sent applications that have a Gmail thread to
// poll, oldest-unchecked-first (an application that was never checked
// always sorts before one that was), capped at limit.
func pendingApplications(all []crm.Application, limit int) []crm.Application {
	var candidates []crm.Application
	for _, app := range all {
		if app.Status == crm.AppStatusSent && app.GmailThreadID != "" {
			candidates = append(candidates, app)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		ai, aj := candidates[i], candidates[j]
		if ai.RepliesCheckedAt.IsZero() != aj.RepliesCheckedAt.IsZero() {
			return ai.RepliesCheckedAt.IsZero() // unchecked (zero) sorts first
		}
		return ai.RepliesCheckedAt.Before(aj.RepliesCheckedAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

// checkApplicationReplies polls one application's Gmail thread and records
// every genuinely new message (skipping ones already in seen) as a
// classified crm.Reply.
func (a *CRMAPI) checkApplicationReplies(ctx context.Context, app crm.Application, seen map[string]bool, stats *checkRepliesStats) error {
	candidate, err := a.store.GetCandidate(app.CandidateID)
	if err != nil {
		return err
	}
	mailbox, ok := mailboxByEmail(candidate, app.FromEmail)
	if !ok {
		return fmt.Errorf("mailbox %q that sent application %s is no longer connected", app.FromEmail, app.ID)
	}
	accessToken, err := a.validAccessToken(ctx, candidate, mailbox)
	if err != nil {
		return err
	}
	messages, err := a.gmail.Replies(ctx, gmail.Token{AccessToken: accessToken}, mailbox.Email, app.GmailThreadID)
	if err != nil {
		return err
	}

	for _, msg := range messages {
		if msg.ID == "" {
			continue
		}
		key := app.ID + "|" + msg.ID
		if seen[key] {
			continue // already recorded by an earlier poll — don't re-classify
		}

		category, summary, classifyErr := a.classifier.Classify(ctx, msg.Subject, msg.Body)
		if classifyErr != nil {
			category, summary = crm.ReplyUnknown, ""
		}
		receivedAt := msg.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}

		_, added, err := a.store.AddReply(crm.Reply{
			ApplicationID:  app.ID,
			GmailMessageID: msg.ID,
			FromEmail:      msg.From,
			Subject:        truncateRunes(msg.Subject, 255),
			Body:           truncateRunes(msg.Body, 60000),
			Category:       category,
			Summary:        truncateRunes(summary, 500),
			ReceivedAt:     receivedAt,
		})
		if err != nil {
			return err
		}
		if added {
			seen[key] = true
			stats.NewReplies++
			stats.ByCategory[category]++
		}
	}
	return nil
}

// mailboxByEmail finds the candidate mailbox that sent from the given
// address, so a reply-check on an already-sent Application polls the exact
// mailbox that sent it (Application.FromEmail snapshots this at send time).
func mailboxByEmail(candidate crm.Candidate, email string) (crm.GmailMailbox, bool) {
	for _, m := range candidate.GmailMailboxes {
		if m.Email == email {
			return m, true
		}
	}
	return crm.GmailMailbox{}, false
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
