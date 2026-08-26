package httpapi

import (
	"net/http"
	"time"

	"faangjobs/internal/crm"
)

func (a *CRMAPI) registerAnalyticsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/crm/analytics", a.analytics)
	mux.HandleFunc("GET /api/crm/replies", a.listReplies)
}

const analyticsWindowDays = 14

type dayPoint struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Sent    int    `json:"sent"`
	Replies int    `json:"replies"`
}

func (a *CRMAPI) analytics(w http.ResponseWriter, r *http.Request) {
	if a.idx == nil {
		writeError(w, http.StatusServiceUnavailable, "job index not available")
		return
	}
	applications, err := a.store.ListApplications()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	replies, err := a.store.ListReplies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	snap := a.idx.Snapshot()
	scraped := len(snap.jobs)
	withEmail := 0
	for _, j := range snap.jobs {
		if j.ApplicationEmail != "" {
			withEmail++
		}
	}

	sent := 0
	for _, app := range applications {
		if app.Status == crm.AppStatusSent {
			sent++
		}
	}

	positive := 0
	classified := 0
	for _, rep := range replies {
		if rep.Category == crm.ReplyUnknown || rep.Category == "" {
			continue
		}
		classified++
		if rep.Category == crm.ReplyGreen {
			positive++
		}
	}

	replyRate := 0.0
	if sent > 0 {
		replyRate = float64(len(replies)) / float64(sent) * 100
	}
	positivityRate := 0.0
	if classified > 0 {
		positivityRate = float64(positive) / float64(classified) * 100
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scrapedJobs":       scraped,
		"scrapedWithEmail":  withEmail,
		"sentApplications":  sent,
		"replies":           len(replies),
		"replyRatePct":      round1(replyRate),
		"positiveReplies":   positive,
		"classifiedReplies": classified,
		"positivityRatePct": round1(positivityRate),
		"timeline":          buildTimeline(applications, replies),
	}, 0)
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// buildTimeline buckets sent applications and received replies per day over
// the last analyticsWindowDays days, oldest first — feeds the frontend's bar
// chart. Every day in the window appears even with a zero count, so the
// chart never has to guess about missing dates.
func buildTimeline(applications []crm.Application, replies []crm.Reply) []dayPoint {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(analyticsWindowDays - 1))

	sentByDay := map[string]int{}
	for _, app := range applications {
		if app.Status != crm.AppStatusSent || app.SentAt.IsZero() {
			continue
		}
		d := app.SentAt.UTC().Truncate(24 * time.Hour)
		if d.Before(start) {
			continue
		}
		sentByDay[d.Format("2006-01-02")]++
	}

	repliesByDay := map[string]int{}
	for _, rep := range replies {
		if rep.ReceivedAt.IsZero() {
			continue
		}
		d := rep.ReceivedAt.UTC().Truncate(24 * time.Hour)
		if d.Before(start) {
			continue
		}
		repliesByDay[d.Format("2006-01-02")]++
	}

	points := make([]dayPoint, analyticsWindowDays)
	for i := 0; i < analyticsWindowDays; i++ {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		points[i] = dayPoint{Date: key, Sent: sentByDay[key], Replies: repliesByDay[key]}
	}
	return points
}

func (a *CRMAPI) listReplies(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListReplies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	category := r.URL.Query().Get("category")
	filtered := items[:0:0]
	for _, rep := range items {
		if category != "" && category != "all" && rep.Category != category {
			continue
		}
		filtered = append(filtered, rep)
	}
	// Most recent first.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	counts := map[string]int{}
	for _, rep := range items {
		counts[rep.Category]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"replies": filtered,
		"total":   len(filtered),
		"counts": map[string]int{
			"green":   counts[crm.ReplyGreen],
			"yellow":  counts[crm.ReplyYellow],
			"red":     counts[crm.ReplyRed],
			"unknown": counts[crm.ReplyUnknown],
		},
	}, 0)
}
