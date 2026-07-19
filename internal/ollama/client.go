// Package ollama implements streaming chat through Ollama's native API.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dicedatalore/oolong/internal/openai"
)

type Client struct {
	url  string
	http *http.Client
}

func New(baseURL string) *Client {
	baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	return &Client{url: baseURL + "/api/chat", http: &http.Client{}}
}

type message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    any       `json:"think,omitempty"`
}

type chunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

func (c *Client) StreamChat(ctx context.Context, model string, messages []openai.Message, opts openai.Options, ch chan<- openai.StreamEvent) {
	defer close(ch)
	emit := func(ev openai.StreamEvent) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	reqBody := request{Model: model, Stream: true}
	if opts.ReasoningEffort == "none" {
		reqBody.Think = false
	} else if opts.ReasoningEffort != "" {
		reqBody.Think = opts.ReasoningEffort
	}
	for _, m := range messages {
		om := message{Role: m.Role, Content: m.Content}
		for _, f := range m.Files {
			om.Content += "\n\n" + fileBlock(f)
		}
		for _, img := range m.Images {
			om.Images = append(om.Images, base64.StdEncoding.EncodeToString(img))
		}
		reqBody.Messages = append(reqBody.Messages, om)
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		emit(openai.StreamEvent{Err: err})
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		emit(openai.StreamEvent{Err: err})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			emit(openai.StreamEvent{Err: fmt.Errorf("ollama: %w", err)})
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		emit(openai.StreamEvent{Err: fmt.Errorf("ollama: %s", e.Error)})
		return
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var part chunk
		if err := json.Unmarshal(scanner.Bytes(), &part); err != nil {
			emit(openai.StreamEvent{Err: fmt.Errorf("ollama: invalid stream: %w", err)})
			return
		}
		if part.Error != "" {
			emit(openai.StreamEvent{Err: fmt.Errorf("ollama: %s", part.Error)})
			return
		}
		if part.Message.Content != "" && !emit(openai.StreamEvent{Delta: part.Message.Content}) {
			return
		}
		if part.Done {
			emit(openai.StreamEvent{Done: true, Usage: openai.Usage{InputTokens: part.PromptEvalCount, OutputTokens: part.EvalCount}})
			return
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		emit(openai.StreamEvent{Err: fmt.Errorf("ollama: %w", err)})
		return
	}
	if ctx.Err() == nil {
		emit(openai.StreamEvent{Err: fmt.Errorf("ollama: stream ended before completion")})
	}
}

func fileBlock(f openai.File) string {
	fence := "```"
	for strings.Contains(f.Text, fence) {
		fence += "`"
	}
	return "File: " + f.Name + "\n" + fence + "\n" + strings.TrimRight(f.Text, "\n") + "\n" + fence
}
