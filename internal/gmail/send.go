package gmail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

const sendURL = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"

// Attachment is one file to attach to a sent message.
type Attachment struct {
	Filename    string
	ContentType string // best-effort guess from the extension if empty
	Data        []byte
}

// SendResult identifies a sent message; ThreadID is what Replies() later
// polls to find the employer's reply.
type SendResult struct {
	MessageID string
	ThreadID  string
}

// Send delivers a message through the Gmail account behind tok, so the
// employer receives it from the candidate's own address rather than ours.
// Callers are responsible for refreshing tok first if it's expired (see
// token.go).
func (c *Client) Send(ctx context.Context, tok Token, fromName, fromEmail, to, subject, body string, attachments []Attachment) (SendResult, error) {
	raw, err := buildMIME(fromName, fromEmail, to, subject, body, attachments)
	if err != nil {
		return SendResult{}, fmt.Errorf("build message: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)})
	if err != nil {
		return SendResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return SendResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("gmail send endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return SendResult{}, fmt.Errorf("gmail send failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return SendResult{}, fmt.Errorf("parse send response: %w", err)
	}
	return SendResult{MessageID: parsed.ID, ThreadID: parsed.ThreadID}, nil
}

func (c *Client) sendEndpoint() string {
	if c.testSendURL != "" {
		return c.testSendURL
	}
	return sendURL
}

// buildMIME assembles an RFC 2822 multipart/mixed message: the direct Go
// port of the reference app's GmailSender::buildMime() — same header order,
// same base64 body/attachment encoding, same RFC 2047 handling for non-ASCII
// From/Subject headers.
func buildMIME(fromName, fromEmail, to, subject, body string, attachments []Attachment) ([]byte, error) {
	from := fromEmail
	if fromName != "" {
		from = mime.QEncoding.Encode("UTF-8", fromName) + " <" + fromEmail + ">"
	}

	boundary, err := randomBoundary()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)

	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	buf.WriteString(chunkedBase64(body))
	buf.WriteString("\r\n")

	for _, att := range attachments {
		if len(att.Data) == 0 {
			continue
		}
		ct := att.ContentType
		if ct == "" {
			ct = mimeTypeFor(att.Filename)
		}
		buf.WriteString("--" + boundary + "\r\n")
		fmt.Fprintf(&buf, "Content-Type: %s; name=\"%s\"\r\n", ct, att.Filename)
		fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", att.Filename)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		buf.WriteString(chunkedBase64(string(att.Data)))
		buf.WriteString("\r\n")
	}

	buf.WriteString("--" + boundary + "--")
	return buf.Bytes(), nil
}

func randomBoundary() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "bnd_" + hex.EncodeToString(b), nil
}

// chunkedBase64 wraps base64 output at 76 characters per line, the RFC 2045
// convention every mail client expects.
func chunkedBase64(s string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	var out strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		out.WriteString(encoded[i:end])
		out.WriteString("\r\n")
	}
	return out.String()
}

func mimeTypeFor(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}
