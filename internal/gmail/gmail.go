// Package gmail implements Google OAuth2 + the Gmail API v1 send/read calls
// needed for the CRM's auto-apply flow, entirely on the standard library
// (net/http, encoding/json, mime/multipart) — Google's OAuth2 and Gmail API
// are plain REST+JSON, no SDK required, consistent with this repo's
// zero-external-Go-dependency design.
package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scopes requested for every candidate connection: send on their behalf,
// read replies, and resolve which address they connected (the reference
// app's exact scope set).
var Scopes = []string{
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/userinfo.email",
	"openid",
}

const (
	authURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL    = "https://oauth2.googleapis.com/token"
	userinfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
)

// Config holds the OAuth client credentials. Empty ClientID means Gmail
// integration is disabled — every caller must check Enabled() first.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (c Config) Enabled() bool { return c.ClientID != "" && c.ClientSecret != "" }

// Client performs the OAuth2 exchange/refresh calls for one Config.
type Client struct {
	cfg  Config
	http *http.Client

	// testTokenURL/testUserinfoURL/testSendURL/testThreadURL override the
	// real Google endpoints in tests, so Exchange/Refresh/FetchEmail/Send/
	// Replies can be exercised against an httptest.Server. Left empty in
	// production.
	testTokenURL    string
	testUserinfoURL string
	testSendURL     string
	testThreadURL   string
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) tokenEndpoint() string {
	if c.testTokenURL != "" {
		return c.testTokenURL
	}
	return tokenURL
}

func (c *Client) userinfoEndpoint() string {
	if c.testUserinfoURL != "" {
		return c.testUserinfoURL
	}
	return userinfoURL
}

// Enabled reports whether this client has usable OAuth credentials.
func (c *Client) Enabled() bool { return c.cfg.Enabled() }

// Token is one candidate's Gmail access grant.
type Token struct {
	AccessToken  string
	RefreshToken string // only returned on the very first exchange (or with prompt=consent)
	Expiry       time.Time
	Scope        string
}

// AuthURL builds the consent-screen URL a candidate should be sent to. state
// round-trips through Google unmodified and is how the callback knows which
// pending connection this is — see internal/httpapi/crm_gmail.go, which uses
// the one-time connect-link token as state.
//
// prompt=consent forces Google to reissue a refresh token even if this
// Google account already granted access before; without it a refresh token
// is only ever handed out once, and a candidate reconnecting after losing
// access would silently get an access-only grant.
func (c *Client) AuthURL(state string) string {
	v := url.Values{
		"client_id":     {c.cfg.ClientID},
		"redirect_uri":  {c.cfg.RedirectURL},
		"response_type": {"code"},
		"scope":         {strings.Join(Scopes, " ")},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return authURL + "?" + v.Encode()
}

// Exchange trades an authorization code (from the OAuth redirect) for a
// token.
func (c *Client) Exchange(ctx context.Context, code string) (Token, error) {
	return c.tokenRequest(ctx, url.Values{
		"code":          {code},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"redirect_uri":  {c.cfg.RedirectURL},
		"grant_type":    {"authorization_code"},
	})
}

// Refresh trades a refresh token for a new access token. Google does not
// reissue the refresh token itself on this call, so the caller must keep the
// one it already has.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	tok, err := c.tokenRequest(ctx, url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"grant_type":    {"refresh_token"},
	})
	if err == nil && tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, err
}

func (c *Client) tokenRequest(ctx context.Context, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenEndpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("google token endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("google token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Token{}, fmt.Errorf("parse token response: %w", err)
	}
	return Token{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
		Scope:        parsed.Scope,
	}, nil
}

// FetchEmail resolves the Gmail address behind an access token, so the CRM
// can show/store which address a candidate actually connected.
func (c *Client) FetchEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userinfoEndpoint(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("google userinfo endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google userinfo failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse userinfo response: %w", err)
	}
	if parsed.Email == "" {
		return "", fmt.Errorf("userinfo response had no email")
	}
	return parsed.Email, nil
}
