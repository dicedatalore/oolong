package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type openaiClient struct {
	api openai.Client
}

func newOpenAIClient(apiKey string, opts ...option.RequestOption) *openaiClient {
	opts = append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(2 * time.Minute),
	}, opts...)
	return &openaiClient{api: openai.NewClient(opts...)}
}

type apiMessage struct {
	Role    string
	Content string
}

type apiUsage struct {
	InputTokens  int
	OutputTokens int
}

// streamEvent is one item of a streamed reply: a content delta, a terminal
// done event carrying usage, or a terminal error.
type streamEvent struct {
	delta string
	usage apiUsage
	done  bool
	err   error
}

// streamChat streams a model response, sending one streamEvent per content
// delta on ch, terminated by a done or err event. It closes ch on return and
// aborts (without a terminal event) if ctx is cancelled.
func (c *openaiClient) streamChat(ctx context.Context, model string, messages []apiMessage, ch chan<- streamEvent) {
	defer close(ch)

	emit := func(ev streamEvent) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, m := range messages {
		input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRole(m.Role)))
	}

	stream := c.api.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		// The Responses API stores responses by default; this client keeps
		// history locally, so opt out.
		Store: openai.Bool(false),
	})
	defer stream.Close()

	var usage apiUsage
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" && !emit(streamEvent{delta: ev.Delta}) {
				return
			}
		case "response.completed":
			usage = apiUsage{
				InputTokens:  int(ev.Response.Usage.InputTokens),
				OutputTokens: int(ev.Response.Usage.OutputTokens),
			}
		case "response.failed", "response.incomplete":
			msg := ev.Response.Error.Message
			if msg == "" {
				msg = "response " + strings.TrimPrefix(ev.Type, "response.")
			}
			emit(streamEvent{err: fmt.Errorf("openai: %s", msg)})
			return
		case "error":
			emit(streamEvent{err: fmt.Errorf("openai: %s", ev.Message)})
			return
		}
	}
	if err := stream.Err(); err != nil {
		emit(streamEvent{err: apiError(err)})
		return
	}
	emit(streamEvent{done: true, usage: usage})
}

// apiError reduces the SDK's verbose API error (method, URL, raw body) to
// just the server's message, which is what the UI shows.
func apiError(err error) error {
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		if apierr.Message != "" {
			return fmt.Errorf("openai: %s", apierr.Message)
		}
		return fmt.Errorf("openai: HTTP %d", apierr.StatusCode)
	}
	return err
}

// validateAPIKey checks the key against the models endpoint, which is free
// and spends no tokens. The API's own 401 message can echo the key back, so
// it is replaced with a generic error.
func validateAPIKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := openai.NewClient(option.WithAPIKey(key))
	_, err := client.Models.List(ctx)
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		if apierr.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("invalid API key")
		}
		return fmt.Errorf("openai: HTTP %d", apierr.StatusCode)
	}
	return err
}
