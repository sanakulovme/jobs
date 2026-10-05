package ailetter

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// ClaudeModel is the Claude model letters are written with.
const ClaudeModel = "claude-opus-5-5"

// Claude writes letters through the Claude API.
type Claude struct {
	client anthropic.Client
}

// NewClaude returns a Claude writer authenticated with apiKey. Extra options (a test
// server's base URL, say) are applied after the defaults.
func NewClaude(apiKey string, opts ...option.RequestOption) *Claude {
	opts = append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(3 * time.Minute),
	}, opts...)
	return &Claude{client: anthropic.NewClient(opts...)}
}

// Write produces the subject and body for one application.
// Write implements the letter writer interface.
func (w *Claude) Write(ctx context.Context, in Input) (Letter, error) {
	var content []anthropic.BetaContentBlockParamUnion

	// The CV comes first and carries the cache breakpoint: one run usually
	// writes several letters for the same candidate, and every one after the
	// first then reads the system prompt + CV from cache.
	cvNote := "No CV is attached; rely on the profile data only."
	if cv := in.CV; cv != nil && isPDF(cv) && len(cv.Data) <= maxCVBytes {
		doc := anthropic.NewBetaDocumentBlock(anthropic.BetaBase64PDFSourceParam{
			Data: base64.StdEncoding.EncodeToString(cv.Data),
		})
		doc.OfDocument.Title = param.NewOpt("CV: " + cv.Filename)
		doc.OfDocument.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
		content = append(content, doc)
		cvNote = "The candidate's CV is the attached document."
	} else if in.CV != nil {
		cvNote = fmt.Sprintf("The CV (%s) is attached to the e-mail but is not a PDF, so you cannot read it; rely on the profile data only.", in.CV.Filename)
	}
	content = append(content, anthropic.NewBetaTextBlock(cvNote+"\n\n"+describe(in)))

	resp, err := w.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     ClaudeModel,
		MaxTokens: 16000,
		System: []anthropic.BetaTextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(content...)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: letterSchema},
		},
		// If a safety classifier declines the request, the API re-serves it
		// on a suitable fallback model inside the same call.
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return Letter{}, fmt.Errorf("AI xat yozolmadi: %w", err)
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return Letter{}, errors.New("AI bu xatni yozishni rad etdi")
	case anthropic.BetaStopReasonMaxTokens:
		return Letter{}, errors.New("AI javobi chegaraga yetib kesildi")
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	return parseLetter(text.String())
}
