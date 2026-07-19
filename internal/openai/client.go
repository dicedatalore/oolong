// Package openai wraps the OpenAI SDK in a minimal streaming chat client.
package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type Client struct {
	api sdk.Client
}

// ChatClient is the provider-neutral streaming surface used by the TUI and
// one-shot mode. Provider packages implement it independently.
type ChatClient interface {
	StreamChat(context.Context, string, []Message, Options, chan<- StreamEvent)
}

func New(apiKey string, opts ...option.RequestOption) *Client {
	// No request timeout: it would bound the whole request including the
	// streamed body, cutting off long responses mid-stream. The user can
	// always stop a stuck stream, which cancels the request context.
	// An empty apiKey sends no Authorization header, which keyless
	// OpenAI-compatible endpoints (Ollama, LM Studio) are fine with.
	if apiKey != "" {
		opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	}
	return &Client{api: sdk.NewClient(opts...)}
}

// WithBaseURL points the client at an OpenAI-compatible endpoint, so callers
// don't need to import the SDK's option package.
func WithBaseURL(url string) option.RequestOption {
	return option.WithBaseURL(url)
}

type Message struct {
	Role    string
	Content string
	Model   string   // assistant messages: the model that produced the reply
	Images  [][]byte // image attachments (PNG/JPEG/GIF/WebP), user messages only
	Files   []File   // text-file attachments, user messages only
}

// File is a text file attached to a user message; it is sent to the model
// as its own content block alongside the message text.
type File struct {
	Name string
	Text string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

// StreamEvent is one item of a streamed reply: a content delta, a terminal
// done event carrying usage, or a terminal error.
type StreamEvent struct {
	Delta string
	Usage Usage
	Done  bool
	Err   error
}

// Options tunes a StreamChat request. Zero values omit the parameter, which
// leaves the server default in effect.
type Options struct {
	ReasoningEffort string // reasoning.effort: none|minimal|low|medium|high
	Verbosity       string // text.verbosity: low|medium|high
}

// StreamChat streams a model response, sending one StreamEvent per content
// delta on ch, terminated by a done or err event. It closes ch on return and
// aborts (without a terminal event) if ctx is cancelled.
func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, opts Options, ch chan<- StreamEvent) {
	defer close(ch)

	emit := func(ev StreamEvent) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, m := range messages {
		role := responses.EasyInputMessageRole(m.Role)
		if len(m.Images) == 0 && len(m.Files) == 0 {
			input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, role))
			continue
		}
		content := make(responses.ResponseInputMessageContentListParam, 0, len(m.Images)+len(m.Files)+1)
		if m.Content != "" {
			content = append(content, responses.ResponseInputContentParamOfInputText(m.Content))
		}
		for _, f := range m.Files {
			content = append(content, responses.ResponseInputContentParamOfInputText(fileBlock(f)))
		}
		for _, img := range m.Images {
			item := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			item.OfInputImage.ImageURL = param.NewOpt("data:" + imageMIME(img) + ";base64," + base64.StdEncoding.EncodeToString(img))
			content = append(content, item)
		}
		input = append(input, responses.ResponseInputItemParamOfMessage(content, role))
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		// The Responses API stores responses by default; this client keeps
		// history locally, so opt out.
		Store: sdk.Bool(false),
	}
	if opts.ReasoningEffort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(opts.ReasoningEffort)}
	}
	if opts.Verbosity != "" {
		params.Text = responses.ResponseTextConfigParam{Verbosity: responses.ResponseTextConfigVerbosity(opts.Verbosity)}
	}
	stream := c.api.Responses.NewStreaming(ctx, params)
	defer stream.Close()

	var usage Usage
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" && !emit(StreamEvent{Delta: ev.Delta}) {
				return
			}
		case "response.completed":
			usage = Usage{
				InputTokens:  int(ev.Response.Usage.InputTokens),
				OutputTokens: int(ev.Response.Usage.OutputTokens),
			}
		case "response.failed", "response.incomplete":
			msg := ev.Response.Error.Message
			if msg == "" {
				msg = "response " + strings.TrimPrefix(ev.Type, "response.")
				// Incomplete responses report why (e.g. max_output_tokens,
				// content_filter) in incomplete_details, not error.
				if reason := ev.Response.IncompleteDetails.Reason; reason != "" {
					msg += ": " + reason
				}
			}
			emit(StreamEvent{Err: fmt.Errorf("openai: %s", msg)})
			return
		case "error":
			emit(StreamEvent{Err: fmt.Errorf("openai: %s", ev.Message)})
			return
		}
	}
	if err := stream.Err(); err != nil {
		emit(StreamEvent{Err: apiError(err)})
		return
	}
	emit(StreamEvent{Done: true, Usage: usage})
}

// fileBlock renders a text attachment as its own content block: a name line
// and a fenced body, the fence grown past any backtick run in the content.
func fileBlock(f File) string {
	fence := "```"
	for strings.Contains(f.Text, fence) {
		fence += "`"
	}
	return "File: " + f.Name + "\n" + fence + "\n" + strings.TrimRight(f.Text, "\n") + "\n" + fence
}

// imageMIME sniffs an image attachment's media type for its data: URL.
// Unrecognized bytes fall back to PNG, which clipboard attachments always
// are; disk attachments are sniffed before they get here.
func imageMIME(data []byte) string {
	if mime := http.DetectContentType(data); strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/png"
}

// ListModels returns the ids of the models available to the API key, for
// checking a user-configured catalog before it is shown in the picker.
func (c *Client) ListModels(ctx context.Context) (map[string]bool, error) {
	ids := make(map[string]bool)
	it := c.api.Models.ListAutoPaging(ctx)
	for it.Next() {
		ids[it.Current().ID] = true
	}
	if err := it.Err(); err != nil {
		return nil, apiError(err)
	}
	return ids, nil
}

// apiError reduces the SDK's verbose API error (method, URL, raw body) to
// just the server's message, which is what the UI shows.
func apiError(err error) error {
	var apierr *sdk.Error
	if errors.As(err, &apierr) {
		if apierr.Message != "" {
			return fmt.Errorf("openai: %s", apierr.Message)
		}
		return fmt.Errorf("openai: HTTP %d", apierr.StatusCode)
	}
	return err
}

// ValidateKey checks the key against the models endpoint, which is free
// and spends no tokens. The API's own 401 message can echo the key back, so
// it is replaced with a generic error.
func ValidateKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := sdk.NewClient(option.WithAPIKey(key))
	_, err := client.Models.List(ctx)
	var apierr *sdk.Error
	if errors.As(err, &apierr) {
		if apierr.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("invalid API key")
		}
		return fmt.Errorf("openai: HTTP %d", apierr.StatusCode)
	}
	return err
}
