package ailetter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
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
func (g *Groq) Write(ctx context.Context, in Input) (Letter, error) {
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

	reqBody, err := json.Marshal(map[string]any{
		"model": g.model,
		"messages": []groqMessage{
			{Role: "system", Content: systemPrompt + "\n\nReply with a JSON object with the keys \"subject\" and \"body\"."},
			{Role: "user", Content: cvNote + "\n\n" + describe(in)},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "application_letter",
				"strict": true,
				"schema": letterSchema,
			},
		},
		"temperature": 0.4,
	})
	if err != nil {
		return Letter{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(reqBody))
	if err != nil {
		return Letter{}, err
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(req)
	if err != nil {
		return Letter{}, fmt.Errorf("AI xat yozolmadi: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Letter{}, fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Letter{}, fmt.Errorf("AI xat yozolmadi: Groq %d: %s", resp.StatusCode, groqErrorMessage(body))
	}

	var out struct {
		Choices []struct {
			Message      groqMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Letter{}, fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
	}
	if len(out.Choices) == 0 {
		return Letter{}, errors.New("AI bo'sh javob qaytardi")
	}
	if out.Choices[0].FinishReason == "length" {
		return Letter{}, errors.New("AI javobi chegaraga yetib kesildi")
	}
	return parseLetter(out.Choices[0].Message.Content)
}

// groqErrorMessage pulls the human-readable message out of an error body.
func groqErrorMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return strings.TrimSpace(string(body))
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
