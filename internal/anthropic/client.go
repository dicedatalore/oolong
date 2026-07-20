// Package anthropic implements streaming chat through Anthropic's Messages API.
package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/dicedatalore/oolong/internal/openai"
)

const defaultMaxTokens = 8192

type Client struct{ api sdk.Client }

func New(apiKey string, opts ...option.RequestOption) *Client {
	if apiKey != "" {
		opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	}
	return &Client{api: sdk.NewClient(opts...)}
}

func WithBaseURL(url string) option.RequestOption { return option.WithBaseURL(url) }

func (c *Client) StreamChat(ctx context.Context, model string, messages []openai.Message, opts openai.Options, ch chan<- openai.StreamEvent) {
	defer close(ch)
	emit := func(event openai.StreamEvent) bool {
		select {
		case ch <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}

	params := sdk.MessageNewParams{Model: sdk.Model(model), MaxTokens: defaultMaxTokens}
	var system []string
	for _, message := range messages {
		if message.Role == "system" {
			system = append(system, message.Content)
			continue
		}
		blocks := make([]sdk.ContentBlockParamUnion, 0, 1+len(message.Files)+len(message.Images))
		if message.Content != "" {
			blocks = append(blocks, sdk.NewTextBlock(message.Content))
		}
		for _, file := range message.Files {
			blocks = append(blocks, sdk.NewTextBlock(fileBlock(file)))
		}
		for _, image := range message.Images {
			blocks = append(blocks, sdk.NewImageBlockBase64(imageMIME(image), base64.StdEncoding.EncodeToString(image)))
		}
		if message.Role == "assistant" {
			params.Messages = append(params.Messages, sdk.NewAssistantMessage(blocks...))
		} else {
			params.Messages = append(params.Messages, sdk.NewUserMessage(blocks...))
		}
	}
	if len(system) > 0 {
		params.System = []sdk.TextBlockParam{{Text: strings.Join(system, "\n\n")}}
	}
	if opts.ReasoningEffort != "" && opts.ReasoningEffort != "none" {
		params.OutputConfig.Effort = sdk.OutputConfigEffort(opts.ReasoningEffort)
	}

	stream := c.api.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var usage openai.Usage
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "message_start":
			u := event.Message.Usage
			usage.InputTokens = int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens)
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				if !emit(openai.StreamEvent{Delta: event.Delta.Text}) {
					return
				}
			}
		case "message_delta":
			u := event.Usage
			if total := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens; total > 0 {
				usage.InputTokens = int(total)
			}
			usage.OutputTokens = int(u.OutputTokens)
		}
	}
	if err := stream.Err(); err != nil {
		if ctx.Err() == nil {
			emit(openai.StreamEvent{Err: apiError(err)})
		}
		return
	}
	emit(openai.StreamEvent{Done: true, Usage: usage})
}

func fileBlock(file openai.File) string {
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
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(apiErr.RawJSON()), &body)
		if body.Error.Message != "" {
			return fmt.Errorf("anthropic: %s", body.Error.Message)
		}
		return fmt.Errorf("anthropic: HTTP %d", apiErr.StatusCode)
	}
	return err
}

// ValidateKey checks an Anthropic key without generating tokens.
func ValidateKey(key string) error {
	return validateKey(key)
}

func ValidateKeyAt(key, baseURL string) error {
	if baseURL == "" {
		return ValidateKey(key)
	}
	return validateKey(key, option.WithBaseURL(baseURL))
}

// validateKey accepts SDK options so tests can use a local server. Production
// callers use ValidateKey, which keeps the real provider endpoint.
func validateKey(key string, opts ...option.RequestOption) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts = append([]option.RequestOption{option.WithAPIKey(key)}, opts...)
	client := sdk.NewClient(opts...)
	_, err := client.Models.List(ctx, sdk.ModelListParams{})
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid Anthropic API key")
	}
	if err != nil {
		return apiError(err)
	}
	return nil
}
