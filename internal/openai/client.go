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

func New(apiKey string, opts ...option.RequestOption) *Client {
	// No request timeout: it would bound the whole request including the
	// streamed body, cutting off long responses mid-stream. The user can
	// always stop a stuck stream, which cancels the request context.
	opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &Client{api: sdk.NewClient(opts...)}
}

type Message struct {
	Role    string
	Content string
	Images  [][]byte // PNG-encoded attachments, user messages only
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

// StreamChat streams a model response, sending one StreamEvent per content
// delta on ch, terminated by a done or err event. It closes ch on return and
// aborts (without a terminal event) if ctx is cancelled.
func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, ch chan<- StreamEvent) {
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
		if len(m.Images) == 0 {
			input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, role))
			continue
		}
		content := make(responses.ResponseInputMessageContentListParam, 0, len(m.Images)+1)
		if m.Content != "" {
			content = append(content, responses.ResponseInputContentParamOfInputText(m.Content))
		}
		for _, img := range m.Images {
			item := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			item.OfInputImage.ImageURL = param.NewOpt("data:image/png;base64," + base64.StdEncoding.EncodeToString(img))
			content = append(content, item)
		}
		input = append(input, responses.ResponseInputItemParamOfMessage(content, role))
	}

	stream := c.api.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		// The Responses API stores responses by default; this client keeps
		// history locally, so opt out.
		Store: sdk.Bool(false),
	})
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
