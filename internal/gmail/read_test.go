package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// b64 encodes a body part the way Gmail's API does: unpadded, URL-safe.
func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func newReadTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(Config{ClientID: "cid", ClientSecret: "secret"})
	c.testThreadURL = srv.URL + "/threads/"
	return c
}

func TestRepliesSkipsOwnMessagesAndPrefersPlainText(t *testing.T) {
	c := newReadTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/threads/thread-1" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{
					"id":           "msg-own",
					"internalDate": "1700000000000",
					"payload": map[string]any{
						"headers": []map[string]string{
							{"name": "From", "value": "Test Candidate <candidate@gmail.com>"},
							{"name": "Subject", "value": "Bewerbung"},
						},
						"mimeType": "text/plain",
						"body":     map[string]string{"data": b64("my own application")},
					},
				},
				{
					"id":           "msg-reply",
					"internalDate": "1700000100000",
					"payload": map[string]any{
						"headers": []map[string]string{
							{"name": "From", "value": "Praxis Meier <info@praxis.de>"},
							{"name": "Subject", "value": "Re: Bewerbung"},
						},
						"mimeType": "multipart/alternative",
						"parts": []map[string]any{
							{
								"mimeType": "text/html",
								"body":     map[string]string{"data": b64("<p>should be skipped</p>")},
							},
							{
								"mimeType": "text/plain",
								"body":     map[string]string{"data": b64("Vielen Dank fuer Ihre Bewerbung.")},
							},
						},
					},
				},
			},
		})
	})

	msgs, err := c.Replies(context.Background(), Token{AccessToken: "at-1"}, "candidate@gmail.com", "thread-1")
	if err != nil {
		t.Fatalf("Replies failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 reply (own message filtered out), got %d: %+v", len(msgs), msgs)
	}
	got := msgs[0]
	if got.ID != "msg-reply" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.From != "info@praxis.de" {
		t.Errorf("From = %q, want bare address extracted from display name", got.From)
	}
	if got.Subject != "Re: Bewerbung" {
		t.Errorf("Subject = %q", got.Subject)
	}
	if got.Body != "Vielen Dank fuer Ihre Bewerbung." {
		t.Errorf("Body = %q, want the text/plain part preferred over text/html", got.Body)
	}
	if got.ReceivedAt.Unix() != 1700000100 {
		t.Errorf("ReceivedAt = %v, want internalDate/1000 as unix seconds", got.ReceivedAt)
	}
}

func TestRepliesFallsBackToHTMLWhenNoPlainTextPart(t *testing.T) {
	c := newReadTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{
					"id": "msg-html-only",
					"payload": map[string]any{
						"headers": []map[string]string{
							{"name": "From", "value": "employer@example.de"},
						},
						"mimeType": "text/html",
						"body":     map[string]string{"data": b64("<p>Wir laden Sie <b>ein</b> &amp; freuen uns.</p>")},
					},
				},
			},
		})
	})

	msgs, err := c.Replies(context.Background(), Token{AccessToken: "at-1"}, "candidate@gmail.com", "thread-2")
	if err != nil {
		t.Fatalf("Replies failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if want := "Wir laden Sie ein & freuen uns."; msgs[0].Body != want {
		t.Errorf("Body = %q, want %q (tags stripped, entities decoded)", msgs[0].Body, want)
	}
}

func TestRepliesReturnsNilOn404(t *testing.T) {
	c := newReadTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	msgs, err := c.Replies(context.Background(), Token{AccessToken: "at-1"}, "candidate@gmail.com", "gone")
	if err != nil {
		t.Fatalf("expected no error for a 404 (deleted thread), got %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil messages for a 404, got %+v", msgs)
	}
}

func TestRepliesFailsOnNon200(t *testing.T) {
	c := newReadTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"insufficient scope"}}`))
	})

	if _, err := c.Replies(context.Background(), Token{AccessToken: "at-1"}, "candidate@gmail.com", "thread-3"); err == nil {
		t.Fatal("expected an error for a non-200/404 response")
	}
}

func TestExtractAddress(t *testing.T) {
	cases := map[string]string{
		"Praxis Meier <info@praxis.de>": "info@praxis.de",
		"info@praxis.de":                "info@praxis.de",
		"":                              "",
		"Not An Address":                "Not An Address",
	}
	for in, want := range cases {
		if got := extractAddress(in); got != want {
			t.Errorf("extractAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
