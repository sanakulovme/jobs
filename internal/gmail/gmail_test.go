package gmail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfigEnabled(t *testing.T) {
	if (Config{}).Enabled() {
		t.Error("empty Config must report Enabled() == false")
	}
	if !(Config{ClientID: "id", ClientSecret: "secret"}).Enabled() {
		t.Error("Config with both id and secret must report Enabled() == true")
	}
	if (Config{ClientID: "id"}).Enabled() {
		t.Error("Config missing a secret must report Enabled() == false")
	}
}

func TestAuthURL(t *testing.T) {
	c := New(Config{ClientID: "cid", ClientSecret: "secret", RedirectURL: "https://example.com/cb"})
	raw := c.AuthURL("tok-123")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthURL produced an invalid URL: %v", err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id":     "cid",
		"redirect_uri":  "https://example.com/cb",
		"response_type": "code",
		"access_type":   "offline",
		"prompt":        "consent",
		"state":         "tok-123",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query param %q = %q, want %q", key, got, want)
		}
	}
	for _, scope := range Scopes {
		if !strings.Contains(q.Get("scope"), scope) {
			t.Errorf("scope param missing %q", scope)
		}
	}
}

// mockGoogle stands in for accounts.google.com/oauth2.googleapis.com for
// Exchange/Refresh/FetchEmail — these must never hit the real network in
// tests.
func mockGoogle(t *testing.T, tokenHandler http.HandlerFunc, userinfoHandler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	if tokenHandler != nil {
		mux.HandleFunc("/token", tokenHandler)
	}
	if userinfoHandler != nil {
		mux.HandleFunc("/userinfo", userinfoHandler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(Config{ClientID: "cid", ClientSecret: "secret", RedirectURL: "https://example.com/cb"})
	// Point at the test server instead of Google's real endpoints.
	return &Client{cfg: c.cfg, http: srv.Client(), testTokenURL: srv.URL + "/token", testUserinfoURL: srv.URL + "/userinfo"}
}

func TestExchangeSuccess(t *testing.T) {
	c := mockGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.Form.Get("code"); got != "auth-code" {
			t.Errorf("code = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600, "scope": "gmail.send",
		})
	}, nil)

	tok, err := c.Exchange(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", tok)
	}
	if tok.Expiry.IsZero() {
		t.Error("expected a non-zero Expiry")
	}
}

func TestExchangeHTTPError(t *testing.T) {
	c := mockGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}, nil)

	if _, err := c.Exchange(context.Background(), "bad-code"); err == nil {
		t.Fatal("expected an error for a non-200 token response")
	}
}

func TestRefreshKeepsExistingRefreshTokenWhenGoogleOmitsIt(t *testing.T) {
	c := mockGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		// Google's refresh response normally has no refresh_token field.
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 3600})
	}, nil)

	tok, err := c.Refresh(context.Background(), "rt-original")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if tok.RefreshToken != "rt-original" {
		t.Errorf("RefreshToken = %q, want the original to be preserved", tok.RefreshToken)
	}
}

func TestFetchEmail(t *testing.T) {
	c := mockGoogle(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("Authorization header = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]string{"email": "candidate@gmail.com"})
	})

	email, err := c.FetchEmail(context.Background(), "at-1")
	if err != nil {
		t.Fatalf("FetchEmail failed: %v", err)
	}
	if email != "candidate@gmail.com" {
		t.Errorf("email = %q", email)
	}
}
