package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeGroq(t *testing.T, status int, reply any, got *map[string]any) *Groq {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(got)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	g := NewGroq("test-key", "")
	g.url = srv.URL
	return g
}

func groqReply(content, finish string) map[string]any {
	return map[string]any{"choices": []map[string]any{{
		"message":       map[string]any{"role": "assistant", "content": content},
		"finish_reason": finish,
	}}}
}

func TestGroqWriteBuildsRequestAndParsesLetter(t *testing.T) {
	var req map[string]any
	g := fakeGroq(t, http.StatusOK, groqReply(`{"subject":"Bewerbung als MFA","body":"Sehr geehrte Frau Weber,\n..."}`, "stop"), &req)

	letter, err := g.Write(context.Background(), testInput(nil))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if letter.Subject != "Bewerbung als MFA" || !strings.HasPrefix(letter.Body, "Sehr geehrte Frau Weber,") {
		t.Errorf("letter = %+v", letter)
	}
	if req["model"] != DefaultGroqModel {
		t.Errorf("model = %v", req["model"])
	}
	rf, _ := req["response_format"].(map[string]any)
	if rf["type"] != "json_schema" || rf["json_schema"].(map[string]any)["strict"] != true {
		t.Errorf("response_format = %v", rf)
	}
	msgs := req["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].(string)
	for _, want := range []string{"No CV is attached", "Dilnoza Karimova", "Praxis Dr. Weber", "Greeting to open with: Sehr geehrte Frau Weber,"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestGroqWriteUnreadableCVFallsBack(t *testing.T) {
	var req map[string]any
	g := fakeGroq(t, http.StatusOK, groqReply(`{"subject":"S","body":"B"}`, "stop"), &req)
	if _, err := g.Write(context.Background(), testInput(&Document{Filename: "cv.docx", Data: []byte("x")})); err != nil {
		t.Fatalf("Write: %v", err)
	}
	user := req["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(user, "could not be extracted") {
		t.Errorf("unreadable CV should be described: %q", user[:120])
	}
}

func TestGroqWriteRetriesTransientFailures(t *testing.T) {
	retryBase = time.Millisecond
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "rate limited"}})
			return
		}
		json.NewEncoder(w).Encode(groqReply(`{"subject":"S","body":"B"}`, "stop"))
	}))
	t.Cleanup(srv.Close)
	g := NewGroq("test-key", "")
	g.url = srv.URL

	if _, err := g.Write(context.Background(), testInput(nil)); err != nil {
		t.Fatalf("Write after two 429s: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestGroqWriteErrors(t *testing.T) {
	var req map[string]any
	g := fakeGroq(t, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "Invalid API Key"}}, &req)
	if _, err := g.Write(context.Background(), testInput(nil)); err == nil || !strings.Contains(err.Error(), "Invalid API Key") {
		t.Errorf("err = %v, want the API's message", err)
	}

	g = fakeGroq(t, http.StatusOK, groqReply(`{"subject":"S","bo`, "length"), &req)
	if _, err := g.Write(context.Background(), testInput(nil)); err == nil {
		t.Error("expected an error for a truncated reply")
	}
}
