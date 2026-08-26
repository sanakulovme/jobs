package crm

import (
	"context"
	"strings"
)

// Classifier sorts an employer's reply into a Reply category with a short
// summary. The reference app calls an LLM (Groq) for this; the default
// implementation here is a documented keyword heuristic so the CRM works
// with zero external dependencies. A FAANGJOBS_GROQ_API_KEY-backed
// implementation can satisfy the same interface later without touching
// callers (see internal/crm/classify_groq.go, added in phase 6c).
type Classifier interface {
	Classify(ctx context.Context, subject, body string) (category, summary string, err error)
}

// KeywordClassifier is a best-effort, LLM-free classifier over German
// rejection/bounce/interview-invite phrases. It is deliberately conservative:
// anything it doesn't recognize stays ReplyUnknown rather than being guessed
// as positive or negative.
type KeywordClassifier struct{}

var (
	redPhrases = []string{
		"leider absagen", "abgesagt", "nicht berücksichtigen", "nicht berücksichtigt",
		"bereits besetzt", "stelle ist besetzt", "vakansiya allaqachon", "kein bedarf",
		"nicht in frage", "absage",
	}
	greenPhrases = []string{
		"vorstellungsgespräch", "zum gespräch ein", "kennenlernen", "interview",
		"gerne einladen", "würden wir uns freuen", "zum kennenlerngespräch",
	}
	yellowPhrases = []string{
		"unzustellbar", "undeliverable", "mailer-daemon", "out of office",
		"abwesenheit", "eingegangen", "danke für ihre bewerbung", "melden uns",
		"prüfen ihre unterlagen", "weitere unterlagen",
	}
)

// Classify implements Classifier.
func (KeywordClassifier) Classify(_ context.Context, subject, body string) (string, string, error) {
	text := strings.ToLower(subject + "\n" + body)
	if strings.TrimSpace(text) == "" {
		return ReplyUnknown, "", nil
	}
	switch {
	case containsAny(text, redPhrases):
		return ReplyRed, "", nil
	case containsAny(text, greenPhrases):
		return ReplyGreen, "", nil
	case containsAny(text, yellowPhrases):
		return ReplyYellow, "", nil
	default:
		return ReplyUnknown, "", nil
	}
}

func containsAny(text string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}
