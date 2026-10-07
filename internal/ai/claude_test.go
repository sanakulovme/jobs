package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"faangjobs/internal/crm"
	"faangjobs/internal/model"
)

// fakeAPI answers one Messages request with a canned structured-output
// reply and records the request body.
func fakeAPI(t *testing.T, stopReason, text string, got *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, got); err != nil {
			t.Errorf("request is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": ClaudeModel,
			"stop_reason": stopReason,
			"content":     []map[string]any{{"type": "text", "text": text}},
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 10},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testInput(cv *Document) LetterInput {
	return LetterInput{
		Job: model.Job{
			ID: "ba~1", Title: "Medizinische Fachangestellte (m/w/d)", Company: "Praxis Dr. Weber",
			Location: "Berlin", Description: "Wir suchen eine MFA mit Erfahrung in der Kardiologie.",
			ContactPerson: "Weber", Salutation: "Frau",
		},
		Candidate: crm.Candidate{FullName: "Dilnoza Karimova", GermanLevel: "B2", Phone: "+998 90 000 00 00", CurrentCountry: "Usbekistan"},
		Profile:   crm.ApplicationProfile{Specialties: []crm.ProfileSpecialty{{Specialty: "kardiologie", ExperienceYears: 3}}},
		CV:        cv,
	}
}

func TestClaudeWriteBuildsRequestAndParsesLetter(t *testing.T) {
	var req map[string]any
	srv := fakeAPI(t, "end_turn", `{"subject":" Bewerbung als MFA – Dilnoza Karimova ","body":"Sehr geehrte Frau Weber,\n..."}`, &req)
	w := NewClaude("test-key", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))

	letter, err := w.Write(context.Background(), testInput(&Document{Filename: "cv.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4")}))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if letter.Subject != "Bewerbung als MFA – Dilnoza Karimova" || !strings.HasPrefix(letter.Body, "Sehr geehrte Frau Weber,") {
		t.Errorf("letter = %+v", letter)
	}

	if req["model"] != ClaudeModel {
		t.Errorf("model = %v", req["model"])
	}
	if req["fallbacks"] != "default" {
		t.Errorf("fallbacks = %v, want \"default\"", req["fallbacks"])
	}
	oc, _ := req["output_config"].(map[string]any)
	if oc["effort"] != "medium" || oc["format"] == nil {
		t.Errorf("output_config = %v", oc)
	}
	msgs, _ := req["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	if first := content[0].(map[string]any); first["type"] != "document" || first["cache_control"] == nil {
		t.Errorf("first block should be the cached CV document, got %v", first["type"])
	}
	prompt := content[len(content)-1].(map[string]any)["text"].(string)
	for _, want := range []string{"Dilnoza Karimova", "B2", "Stated experience: 3 years in Kardiologie", "Praxis Dr. Weber", "Kardiologie", "Greeting to open with: Sehr geehrte Frau Weber,"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestClaudeWriteSkipsNonPDFCV(t *testing.T) {
	var req map[string]any
	srv := fakeAPI(t, "end_turn", `{"subject":"S","body":"B"}`, &req)
	w := NewClaude("test-key", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))

	if _, err := w.Write(context.Background(), testInput(&Document{Filename: "cv.docx", ContentType: "application/msword", Data: []byte("x")})); err != nil {
		t.Fatalf("Write: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 1 || !strings.Contains(content[0].(map[string]any)["text"].(string), "not a PDF") {
		t.Errorf("non-PDF CV should be described, not attached: %v", content)
	}
}

func TestClaudeWriteReportsRefusal(t *testing.T) {
	var req map[string]any
	srv := fakeAPI(t, "refusal", "", &req)
	w := NewClaude("test-key", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if _, err := w.Write(context.Background(), testInput(nil)); err == nil {
		t.Fatal("expected an error for a refusal")
	}
}
