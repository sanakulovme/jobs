package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const threadURLPrefix = "https://gmail.googleapis.com/gmail/v1/users/me/threads/"

// Message is one email found in a Gmail thread, already decoded to plain
// text with headers pulled out.
type Message struct {
	ID         string
	From       string // bare address, e.g. "info@praxis.de"
	Subject    string
	Body       string
	ReceivedAt time.Time
}

// Replies returns every message in threadID that wasn't sent by ownAddress
// (the candidate's own connected Gmail address). Employers often reply from
// a different mailbox than the one the application was sent to, so this
// reads the whole thread rather than matching on a sender address — the
// same design as the reference app's GmailReader::replies(). A thread that
// no longer exists (HTTP 404, e.g. deleted from the mailbox) returns
// (nil, nil), not an error.
func (c *Client) Replies(ctx context.Context, tok Token, ownAddress, threadID string) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.threadEndpoint(threadID)+"?format=full", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gmail threads endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gmail thread fetch failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Messages []gmailMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse thread response: %w", err)
	}

	own := strings.ToLower(strings.TrimSpace(ownAddress))
	var out []Message
	for _, m := range parsed.Messages {
		headers := headerMap(m.Payload.Headers)
		from := headers["from"]
		if own != "" && strings.Contains(strings.ToLower(from), own) {
			continue // the candidate's own sent message, not a reply
		}
		out = append(out, Message{
			ID:         m.ID,
			From:       extractAddress(from),
			Subject:    headers["subject"],
			Body:       messageBody(m.Payload),
			ReceivedAt: parseInternalDate(m.InternalDate),
		})
	}
	return out, nil
}

func (c *Client) threadEndpoint(threadID string) string {
	if c.testThreadURL != "" {
		return c.testThreadURL + threadID
	}
	return threadURLPrefix + threadID
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailPart struct {
	MimeType string        `json:"mimeType"`
	Headers  []gmailHeader `json:"headers"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

type gmailMessage struct {
	ID           string    `json:"id"`
	InternalDate string    `json:"internalDate"`
	Payload      gmailPart `json:"payload"`
}

func headerMap(headers []gmailHeader) map[string]string {
	out := make(map[string]string, len(headers))
	for _, h := range headers {
		out[strings.ToLower(h.Name)] = h.Value
	}
	return out
}

// messageBody prefers the first text/plain part found anywhere in the
// (arbitrarily nested) part tree; if the sender only provided HTML, tags are
// stripped and entities decoded as a fallback.
func messageBody(payload gmailPart) string {
	if plain, ok := findPart(payload, "text/plain"); ok {
		return plain
	}
	if htmlPart, ok := findPart(payload, "text/html"); ok {
		return strings.TrimSpace(html.UnescapeString(stripTagsRe.ReplaceAllString(htmlPart, "")))
	}
	return ""
}

var stripTagsRe = regexp.MustCompile(`<[^>]*>`)

func findPart(part gmailPart, mimeType string) (string, bool) {
	if part.MimeType == mimeType && part.Body.Data != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			return "", false
		}
		return string(decoded), true
	}
	for _, child := range part.Parts {
		if body, ok := findPart(child, mimeType); ok {
			return body, true
		}
	}
	return "", false
}

var addressRe = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)

// extractAddress pulls the bare address out of a "Display Name <addr>" From
// header, falling back to the raw (trimmed) header value if no address
// pattern is found.
func extractAddress(from string) string {
	if m := addressRe.FindString(from); m != "" {
		return m
	}
	return strings.TrimSpace(from)
}

func parseInternalDate(raw string) time.Time {
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
