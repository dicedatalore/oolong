// Package google implements streaming chat through Google's Gemini API.
package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/dicedatalore/oolong/internal/chat"
)

// Client streams Gemini responses. The genai client is rebuilt per request:
// construction is local (no network) and needs a context, which only the
// request supplies.
type Client struct {
	apiKey  string
	baseURL string
}

type Option func(*Client)

func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

func New(apiKey string, opts ...Option) *Client {
	c := &Client{apiKey: apiKey}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) api(ctx context.Context) (*genai.Client, error) {
	cfg := &genai.ClientConfig{APIKey: c.apiKey, Backend: genai.BackendGeminiAPI}
	cfg.HTTPOptions.BaseURL = c.baseURL
	return genai.NewClient(ctx, cfg)
}

func (c *Client) StreamChat(ctx context.Context, model string, messages []chat.Message, opts chat.Options, ch chan<- chat.StreamEvent) {
	defer close(ch)
	emit := func(event chat.StreamEvent) bool {
		select {
		case ch <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}

	api, err := c.api(ctx)
	if err != nil {
		emit(chat.StreamEvent{Err: fmt.Errorf("google: %v", err)})
		return
	}

	config := &genai.GenerateContentConfig{}
	var system []string
	contents := make([]*genai.Content, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			system = append(system, message.Content)
			continue
		}
		parts := make([]*genai.Part, 0, 1+len(message.Files)+len(message.Images))
		if message.Content != "" {
			parts = append(parts, genai.NewPartFromText(message.Content))
		}
		for _, file := range message.Files {
			parts = append(parts, genai.NewPartFromText(fileBlock(file)))
		}
		for _, image := range message.Images {
			parts = append(parts, genai.NewPartFromBytes(image, imageMIME(image)))
		}
		role := genai.RoleUser
		if message.Role == "assistant" {
			role = genai.RoleModel
		}
		contents = append(contents, genai.NewContentFromParts(parts, genai.Role(role)))
	}
	if len(system) > 0 {
		config.SystemInstruction = genai.NewContentFromText(strings.Join(system, "\n\n"), genai.RoleUser)
	}
	if level := thinkingLevel(opts.ReasoningEffort); level != "" {
		config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: level}
	}

	var usage chat.Usage
	for resp, err := range api.Models.GenerateContentStream(ctx, model, contents, config) {
		if err != nil {
			if ctx.Err() == nil {
				emit(chat.StreamEvent{Err: apiError(err)})
			}
			return
		}
		if u := resp.UsageMetadata; u != nil {
			if u.PromptTokenCount > 0 {
				usage.InputTokens = int(u.PromptTokenCount)
			}
			if total := u.CandidatesTokenCount + u.ThoughtsTokenCount; total > 0 {
				usage.OutputTokens = int(total)
			}
		}
		if delta := text(resp); delta != "" {
			if !emit(chat.StreamEvent{Delta: delta}) {
				return
			}
		}
	}
	emit(chat.StreamEvent{Done: true, Usage: usage})
}

// text concatenates a chunk's visible text parts. The SDK's Text() helper is
// avoided: it logs warnings to the standard logger, which would scribble over
// the TUI.
func text(resp *genai.GenerateContentResponse) string {
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if part != nil && !part.Thought {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// thinkingLevel maps the shared effort ladder onto Gemini's thinking levels.
// Gemini has no "none": minimal is as low as thinking goes, and its ladder
// tops out at high. Unknown values pass through uppercased, so the API can
// report them like the other providers do.
func thinkingLevel(effort string) genai.ThinkingLevel {
	switch effort {
	case "":
		return ""
	case "none":
		return genai.ThinkingLevelMinimal
	case "low":
		return genai.ThinkingLevelLow
	case "medium":
		return genai.ThinkingLevelMedium
	case "high", "xhigh":
		return genai.ThinkingLevelHigh
	}
	return genai.ThinkingLevel(strings.ToUpper(effort))
}

func fileBlock(file chat.File) string {
	fence := "```"
	for strings.Contains(file.Text, fence) {
		fence += "`"
	}
	return "File: " + file.Name + "\n" + fence + "\n" + strings.TrimRight(file.Text, "\n") + "\n" + fence
}

func imageMIME(data []byte) string {
	if mime := http.DetectContentType(data); strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/png"
}

func apiError(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Message != "" {
			return fmt.Errorf("google: %s", apiErr.Message)
		}
		return fmt.Errorf("google: HTTP %d", apiErr.Code)
	}
	return err
}

// ValidateKey checks a Google API key without generating tokens.
func ValidateKey(key string) error {
	return validateKey(key, "")
}

func ValidateKeyAt(key, baseURL string) error { return validateKey(key, baseURL) }

// validateKey accepts a base URL so tests can use a local server. Production
// callers use ValidateKey, which keeps the real Gemini endpoint.
func validateKey(key, baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI}
	cfg.HTTPOptions.BaseURL = baseURL
	client, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("google: %v", err)
	}
	_, err = client.Models.List(ctx, nil)
	var apiErr genai.APIError
	if errors.As(err, &apiErr) &&
		(apiErr.Code == http.StatusBadRequest || apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden) {
		return fmt.Errorf("invalid Google API key")
	}
	if err != nil {
		return apiError(err)
	}
	return nil
}
