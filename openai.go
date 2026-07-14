package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	chatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	modelsURL          = "https://api.openai.com/v1/models"
)

type openaiClient struct {
	apiKey string
	http   *http.Client
}

func newOpenAIClient(apiKey string) *openaiClient {
	return &openaiClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 2 * time.Minute},
	}
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []apiMessage   `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type apiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// chatErrorResponse is the shape of a non-200 (non-SSE) failure body.
type chatErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *apiUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamEvent is one item of a streamed reply: a content delta, a terminal
// done event carrying usage, or a terminal error.
type streamEvent struct {
	delta string
	usage apiUsage
	done  bool
	err   error
}

// streamChat streams a chat completion, sending one streamEvent per content
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

	body, err := json.Marshal(chatRequest{
		Model:         model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	})
	if err != nil {
		emit(streamEvent{err: err})
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		emit(streamEvent{err: err})
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		emit(streamEvent{err: err})
		return
	}
	defer resp.Body.Close()

	// Failures come back as a plain JSON error object, not an SSE stream.
	if resp.StatusCode != http.StatusOK {
		var parsed chatErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err == nil && parsed.Error != nil {
			emit(streamEvent{err: fmt.Errorf("openai: %s", parsed.Error.Message)})
		} else {
			emit(streamEvent{err: fmt.Errorf("openai: HTTP %d", resp.StatusCode)})
		}
		return
	}

	var usage apiUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			emit(streamEvent{err: fmt.Errorf("decoding stream: %w", err)})
			return
		}
		if chunk.Error != nil {
			emit(streamEvent{err: fmt.Errorf("openai: %s", chunk.Error.Message)})
			return
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if !emit(streamEvent{delta: chunk.Choices[0].Delta.Content}) {
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		emit(streamEvent{err: err})
		return
	}
	emit(streamEvent{done: true, usage: usage})
}

// validateAPIKey checks the key against the models endpoint, which is free
// and spends no tokens. The API's own 401 message can echo the key back, so
// it is replaced with a generic error.
func validateAPIKey(key string) error {
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("invalid API key")
	default:
		return fmt.Errorf("openai: HTTP %d", resp.StatusCode)
	}
}
