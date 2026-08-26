package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"faangjobs/internal/crm"
)

// registerGmailRoutes wires both the authenticated admin actions
// (generate/revoke a connect link) and the two PUBLIC routes a candidate's
// own browser hits — /gmail-connect/{token} and the OAuth callback — which
// must work without a 42.uz login, since the candidate isn't a CRM user.
// server.go's auth.Middleware carries an explicit bypass for both paths.
func (a *CRMAPI) registerGmailRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/crm/candidates/{id}/gmail/connect-link", a.createGmailConnectLink)
	mux.HandleFunc("POST /api/crm/candidates/{id}/gmail/disconnect", a.disconnectGmail)

	mux.HandleFunc("GET /gmail-connect/{token}", a.startGmailConnect)
	mux.HandleFunc("GET /api/crm/gmail/callback", a.gmailCallback)
}

// createGmailConnectLink mints a one-time link the admin sends to the
// candidate (Telegram, WhatsApp, ...) outside the CRM — clicking it drives
// the OAuth consent screen as *that candidate*, which is the whole point:
// the admin's own Gmail must never be the one that gets connected.
func (a *CRMAPI) createGmailConnectLink(w http.ResponseWriter, r *http.Request) {
	if !a.gmail.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "Gmail integratsiyasi sozlanmagan (Google Client ID/Secret berilmagan)")
		return
	}
	candidateID := r.PathValue("id")
	if _, err := a.store.GetCandidate(candidateID); err != nil {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	t, err := a.store.CreateGmailConnectToken(candidateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	link := fmt.Sprintf("%s://%s/gmail-connect/%s", schemeOf(r), r.Host, t.Token)
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":       link,
		"expiresAt": t.ExpiresAt,
	}, 0)
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

func (a *CRMAPI) disconnectGmail(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.ClearCandidateGmail(r.PathValue("id"))
	if errors.Is(err, crm.ErrNotFound()) {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c.Redacted(), 0)
}

// startGmailConnect is the public landing page: validate the token, then
// send the candidate's browser straight to Google's consent screen with the
// token itself as OAuth state (single-use, unguessable, already tied to one
// candidate — no separate CSRF token needed).
func (a *CRMAPI) startGmailConnect(w http.ResponseWriter, r *http.Request) {
	if !a.gmail.Enabled() {
		writeGmailStatusPage(w, "Xatolik", "Gmail integratsiyasi hali sozlanmagan.", false)
		return
	}
	token := r.PathValue("token")
	t, err := a.store.ResolveGmailConnectToken(token)
	if err != nil {
		writeGmailStatusPage(w, "Havola yaroqsiz", err.Error(), false)
		return
	}
	candidate, err := a.store.GetCandidate(t.CandidateID)
	if err != nil {
		writeGmailStatusPage(w, "Xatolik", "Kandidat topilmadi.", false)
		return
	}
	http.Redirect(w, r, buildConnectPage(candidate.FullName, a.gmail.AuthURL(token)), http.StatusFound)
}

// buildConnectPage is a tiny pass-through: for phase 6a we redirect straight
// to Google rather than showing an intermediate "continue" page, keeping the
// click count at one. Kept as a named function so a confirmation page (naming
// which CRM is asking, e.g. against phishing worries) is a one-line change
// later without touching the handler logic above.
func buildConnectPage(_ string, googleAuthURL string) string { return googleAuthURL }

// gmailCallback is where Google redirects after the candidate approves (or
// denies) access. Must live at exactly the path registered in Google Cloud
// Console as the redirect URI.
func (a *CRMAPI) gmailCallback(w http.ResponseWriter, r *http.Request) {
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		writeGmailStatusPage(w, "Ulanmadi", "Google tomonidan rad etildi: "+errMsg, false)
		return
	}
	code := r.URL.Query().Get("code")
	token := r.URL.Query().Get("state")
	if code == "" || token == "" {
		writeGmailStatusPage(w, "Xatolik", "Google'dan kutilgan ma'lumotlar kelmadi.", false)
		return
	}

	t, err := a.store.ResolveGmailConnectToken(token)
	if err != nil {
		writeGmailStatusPage(w, "Havola yaroqsiz", err.Error(), false)
		return
	}

	tok, err := a.gmail.Exchange(r.Context(), code)
	if err != nil {
		writeGmailStatusPage(w, "Xatolik", "Google bilan almashinuv muvaffaqiyatsiz: "+err.Error(), false)
		return
	}
	email, err := a.gmail.FetchEmail(r.Context(), tok.AccessToken)
	if err != nil {
		writeGmailStatusPage(w, "Xatolik", "Gmail manzilini aniqlab bo'lmadi: "+err.Error(), false)
		return
	}
	if tok.RefreshToken == "" {
		// Google only omits this when the account already has a live grant
		// for this client and prompt=consent somehow didn't force a fresh
		// one — without it we can never refresh, so treat it as a failure
		// rather than silently saving a connection that expires in an hour.
		writeGmailStatusPage(w, "Xatolik", "Google \"refresh token\" bermadi — qaytadan urinib ko'ring.", false)
		return
	}

	if _, err := a.store.SetCandidateGmail(t.CandidateID, email, tok.AccessToken, tok.RefreshToken, tok.Scope, tok.Expiry); err != nil {
		writeGmailStatusPage(w, "Xatolik", "Saqlashda xatolik: "+err.Error(), false)
		return
	}
	_ = a.store.MarkGmailConnectTokenUsed(token)

	writeGmailStatusPage(w, "Ulandi", fmt.Sprintf("Gmail hisobingiz (%s) muvaffaqiyatli ulandi. Bu oynani yopishingiz mumkin.", email), true)
}

// validAccessToken returns a usable access token for candidate, refreshing
// and persisting it first if the stored one has expired (or is within a
// minute of expiring) — the same slack the reference app's GmailSender
// gives itself so a token can't lapse mid-request.
func (a *CRMAPI) validAccessToken(ctx context.Context, candidate crm.Candidate) (string, error) {
	if !candidate.GmailConnected() {
		return "", fmt.Errorf("kandidatning Gmail hisobi ulanmagan")
	}
	if candidate.GmailAccessToken != "" && time.Now().Add(time.Minute).Before(candidate.GmailTokenExpiresAt) {
		return candidate.GmailAccessToken, nil
	}

	tok, err := a.gmail.Refresh(ctx, candidate.GmailRefreshToken)
	if err != nil {
		return "", fmt.Errorf("Gmail ruxsati muddati tugagan, kandidat qayta ulashi kerak: %w", err)
	}
	if _, err := a.store.SetCandidateGmail(candidate.ID, candidate.GmailEmail, tok.AccessToken, tok.RefreshToken, tok.Scope, tok.Expiry); err != nil {
		return "", fmt.Errorf("token yangilanishini saqlashda xatolik: %w", err)
	}
	return tok.AccessToken, nil
}

// writeGmailStatusPage renders a minimal, dependency-free HTML page for the
// two public routes above — these are seen once per candidate, not worth a
// React bundle for.
func writeGmailStatusPage(w http.ResponseWriter, title, message string, ok bool) {
	color := "#d44c47"
	if ok {
		color = "#448361"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;background:#faf9f7;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.box{max-width:420px;padding:32px;border-radius:12px;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.08)}
h1{font-size:20px;color:%s;margin:0 0 8px}p{color:#444;line-height:1.5}</style></head>
<body><div class="box"><h1>%s</h1><p>%s</p></div></body></html>`, title, color, title, message)
}
