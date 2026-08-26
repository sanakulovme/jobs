package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// part is a fully-drained copy of a multipart.Part: the real *multipart.Part
// is only readable until the next NextPart() call, so parseMIME reads each
// one's content immediately rather than handing back Parts for the caller to
// read later (a classic mime/multipart footgun).
type part struct {
	header   textproto.MIMEHeader
	filename string
	data     []byte
}

// parseMIME re-parses buildMIME's output with the stdlib's own MIME reader —
// the best way to confirm the message we hand-build is actually well-formed,
// not just "looks right" by eye.
func parseMIME(t *testing.T, raw []byte) (headers map[string][]string, parts []part) {
	t.Helper()
	msg := string(raw)
	headerEnd := strings.Index(msg, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatalf("no header/body separator found in message:\n%s", msg)
	}
	headerText := msg[:headerEnd]

	headers = map[string][]string{}
	var boundary string
	for _, line := range strings.Split(headerText, "\r\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		headers[k] = append(headers[k], v)
		if k == "Content-Type" {
			_, params, err := mime.ParseMediaType(v)
			if err != nil {
				t.Fatalf("parse Content-Type: %v", err)
			}
			boundary = params["boundary"]
		}
	}
	if boundary == "" {
		t.Fatal("no multipart boundary found in Content-Type header")
	}

	mr := multipart.NewReader(bytes.NewReader(raw[headerEnd+4:]), boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		// Each part is base64-encoded (Content-Transfer-Encoding: base64),
		// which mime/multipart does NOT auto-decode (unlike quoted-printable)
		// — decode it ourselves to get back the original bytes.
		raw, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(raw), "\r\n", ""))
		if err != nil {
			t.Fatalf("decode base64 part body: %v", err)
		}
		parts = append(parts, part{header: p.Header, filename: p.FileName(), data: decoded})
	}
	return headers, parts
}

func TestBuildMIMEBodyOnly(t *testing.T) {
	raw, err := buildMIME("Abbos Sanakulov", "abbos@gmail.com", "praxis@example.de", "Bewerbung als MFA", "Sehr geehrte Damen und Herren,\n\ntext", nil)
	if err != nil {
		t.Fatalf("buildMIME failed: %v", err)
	}
	headers, parts := parseMIME(t, raw)

	if !strings.Contains(headers["To"][0], "praxis@example.de") {
		t.Errorf("To header = %v", headers["To"])
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (body only), got %d", len(parts))
	}
	if !strings.Contains(string(parts[0].data), "Sehr geehrte Damen und Herren") {
		t.Errorf("body part missing expected text, got: %q", parts[0].data)
	}
}

func TestBuildMIMEWithAttachments(t *testing.T) {
	attachments := []Attachment{
		{Filename: "cv.pdf", Data: []byte("%PDF-1.4 fake cv content")},
		{Filename: "anschreiben.docx", Data: []byte("fake docx content")},
	}
	raw, err := buildMIME("Test Candidate", "test@gmail.com", "employer@example.de", "Bewerbung", "body text", attachments)
	if err != nil {
		t.Fatalf("buildMIME failed: %v", err)
	}
	_, parts := parseMIME(t, raw)

	if len(parts) != 3 { // body + 2 attachments
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	cvPart := parts[1]
	if cvPart.filename != "cv.pdf" {
		t.Errorf("attachment[0] filename = %q, want cv.pdf", cvPart.filename)
	}
	if ct := cvPart.header.Get("Content-Type"); !strings.Contains(ct, "application/pdf") {
		t.Errorf("attachment[0] Content-Type = %q, want application/pdf", ct)
	}
	if string(cvPart.data) != "%PDF-1.4 fake cv content" {
		t.Errorf("attachment[0] content corrupted: %q", cvPart.data)
	}

	docxPart := parts[2]
	if docxPart.filename != "anschreiben.docx" {
		t.Errorf("attachment[1] filename = %q", docxPart.filename)
	}
	if ct := docxPart.header.Get("Content-Type"); !strings.Contains(ct, "wordprocessingml") {
		t.Errorf("attachment[1] Content-Type = %q, want a docx MIME type", ct)
	}
}

func TestBuildMIMEEncodesNonASCIIHeaders(t *testing.T) {
	raw, err := buildMIME("Müller Öztürk", "test@gmail.com", "to@example.de", "Bewerbung für Größe", "körper", nil)
	if err != nil {
		t.Fatalf("buildMIME failed: %v", err)
	}
	msg := string(raw)
	// Raw UTF-8 bytes must never appear unencoded in a header line — RFC 2047
	// encoded-words (=?UTF-8?...?=) are required.
	headerEnd := strings.Index(msg, "\r\n\r\n")
	headerText := msg[:headerEnd]
	if strings.ContainsAny(headerText, "üöÖß") {
		t.Errorf("header section contains raw non-ASCII bytes, want RFC 2047 encoding:\n%s", headerText)
	}
	if !strings.Contains(headerText, "=?utf-8?") && !strings.Contains(headerText, "=?UTF-8?") {
		t.Errorf("expected an RFC 2047 encoded-word in headers:\n%s", headerText)
	}
}

// TestSendPostsRawMessage verifies Send() talks to the Gmail endpoint
// correctly (auth header, base64url raw field) against a local test server —
// never the real network.
func TestSendPostsRawMessage(t *testing.T) {
	var gotAuth, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Raw string `json:"raw"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotRaw = body.Raw
		json.NewEncoder(w).Encode(map[string]string{"id": "msg-1", "threadId": "thread-1"})
	}))
	defer srv.Close()

	c := New(Config{ClientID: "cid", ClientSecret: "secret"})
	c.testSendURL = srv.URL

	result, err := c.Send(context.Background(), Token{AccessToken: "at-1"}, "Test", "test@gmail.com", "to@example.de", "Subj", "Body", nil)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if result.MessageID != "msg-1" || result.ThreadID != "thread-1" {
		t.Errorf("result = %+v", result)
	}
	if gotAuth != "Bearer at-1" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotRaw == "" {
		t.Error("expected a non-empty base64url raw field")
	}
}

func TestSendFailsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"insufficient scope"}}`))
	}))
	defer srv.Close()

	c := New(Config{ClientID: "cid", ClientSecret: "secret"})
	c.testSendURL = srv.URL

	if _, err := c.Send(context.Background(), Token{AccessToken: "at-1"}, "Test", "test@gmail.com", "to@example.de", "Subj", "Body", nil); err == nil {
		t.Fatal("expected an error for a non-200 send response")
	}
}
