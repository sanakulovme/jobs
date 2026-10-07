package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultGroqModel supports strict JSON-schema output on Groq, which is what
// guarantees a parseable {subject, body} reply.
const DefaultGroqModel = "openai/gpt-oss-120b"

const groqURL = "https://api.groq.com/openai/v1/chat/completions"

// maxCVTextChars bounds the CV text put in the prompt; a real CV is a few
// thousand characters, so this only stops a mis-uploaded book.
const maxCVTextChars = 30000

// Groq writes letters through Groq's OpenAI-compatible chat API. Groq
// models can't read PDFs, so the CV is converted to text first (pdftotext,
// from poppler-utils).
type Groq struct {
	apiKey string
	model  string
	url    string
	http   *http.Client
}

// NewGroq returns a Groq writer; model "" uses DefaultGroqModel.
func NewGroq(apiKey, model string) *Groq {
	if model == "" {
		model = DefaultGroqModel
	}
	return &Groq{apiKey: apiKey, model: model, url: groqURL, http: &http.Client{Timeout: 2 * time.Minute}}
}

// Model reports which Groq model letters are written with.
func (g *Groq) Model() string { return g.model }

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Write implements the letter writer interface.
func (g *Groq) Write(ctx context.Context, in LetterInput) (Letter, error) {
	cvNote := "No CV is attached; rely on the profile data only."
	if cv := in.CV; cv != nil {
		cvNote = fmt.Sprintf("The CV (%s) is attached to the e-mail but its text could not be extracted; rely on the profile data only.", cv.Filename)
		if isPDF(cv) && len(cv.Data) <= maxCVBytes {
			if text, err := pdfText(ctx, cv.Data); err == nil && text != "" {
				if len(text) > maxCVTextChars {
					text = text[:maxCVTextChars]
				}
				cvNote = "<cv>\n" + text + "\n</cv>"
			}
		}
	}

	text, err := g.chatJSON(ctx,
		systemPrompt+"\n\nReply with a JSON object with the keys \"subject\" and \"body\".",
		cvNote+"\n\n"+describe(in),
		"application_letter", letterSchema)
	if err != nil {
		return Letter{}, err
	}
	return parseLetter(text)
}

// chatJSON runs one chat completion whose reply is constrained to schema
// (Groq strict JSON-schema mode) and returns the raw JSON text.
func (g *Groq) chatJSON(ctx context.Context, system, user, schemaName string, schema map[string]any) (string, error) {
	params := map[string]any{
		"model": g.model,
		"messages": []groqMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   schemaName,
				"strict": true,
				"schema": schema,
			},
		},
		"temperature": 0.2,
		// Room for a long job list plus the reasoning tokens gpt-oss spends
		// first; Groq's default cap cut real listing pages off mid-JSON.
		"max_completion_tokens": 32768,
	}
	// gpt-oss is a reasoning model. "high" made Groq's strict-JSON check
	// fail on most letters in live tests, so stay at medium.
	if strings.HasPrefix(g.model, "openai/gpt-oss") {
		params["reasoning_effort"] = "medium"
	}
	reqBody, err := json.Marshal(params)
	if err != nil {
		return "", err
	}

	body, err := g.post(ctx, reqBody)
	if err != nil {
		return "", err
	}

	var out struct {
		Choices []struct {
			Message      groqMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("AI bo'sh javob qaytardi")
	}
	if out.Choices[0].FinishReason == "length" {
		return "", errors.New("AI javobi chegaraga yetib kesildi")
	}
	return out.Choices[0].Message.Content, nil
}

// groqAttempts is how many times one letter request is tried. Retries
// cover what a busy free tier and a flaky network actually produce:
// connection errors, 429 rate limits and 5xx responses.
const groqAttempts = 4

// retryBase scales the backoff; tests shrink it.
var retryBase = 2 * time.Second

// post sends one chat-completions request, retrying transient failures with
// backoff (or the server's Retry-After, capped), and returns the 200 body.
func (g *Groq) post(ctx context.Context, reqBody []byte) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= groqAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, backoff(attempt, lastErr)); err != nil {
				return nil, lastErr
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("AI xat yozolmadi: %w", err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		msg, code := groqError(body)
		apiErr := &groqStatusError{status: resp.StatusCode, msg: msg, retryAfter: resp.Header.Get("Retry-After")}
		// A generation that failed Groq's JSON-schema check is a sampling
		// fluke, not a bad request, so it is retried like a 429.
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 && code != "json_validate_failed" {
			return nil, apiErr // a bad key or a bad request won't fix itself
		}
		lastErr = apiErr
	}
	return nil, lastErr
}

type groqStatusError struct {
	status     int
	msg        string
	retryAfter string
}

func (e *groqStatusError) Error() string {
	return fmt.Sprintf("AI xat yozolmadi: Groq %d: %s", e.status, e.msg)
}

// backoff waits 2s, 4s, 8s… or what a 429's Retry-After asks, up to 30s.
func backoff(attempt int, lastErr error) time.Duration {
	wait := retryBase << (attempt - 2)
	var se *groqStatusError
	if errors.As(lastErr, &se) && se.retryAfter != "" {
		if secs, err := strconv.ParseFloat(se.retryAfter, 64); err == nil && secs > 0 {
			wait = time.Duration(secs * float64(time.Second))
		}
	}
	return min(wait, 30*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// groqError pulls the human-readable message and the error code out of an
// error body.
func groqError(body []byte) (msg, code string) {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message, e.Error.Code
	}
	return strings.TrimSpace(string(body)), ""
}

// pdfText extracts a PDF's text with pdftotext (poppler-utils). It fails
// cleanly when the tool isn't installed or the PDF is a scan with no text
// layer, and the caller then falls back to the profile data alone.
func pdfText(ctx context.Context, data []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
