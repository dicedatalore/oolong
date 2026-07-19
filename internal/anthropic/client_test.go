package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/dicedatalore/oolong/internal/openai"
)

func sse(w io.Writer, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func TestStreamChat(t *testing.T) {
	var path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, "message_start", `{"type":"message_start","message":{"usage":{"input_tokens":7,"cache_creation_input_tokens":2,"cache_read_input_tokens":1}}}`)
		sse(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`)
		sse(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Claude"}}`)
		sse(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)
		sse(w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer srv.Close()

	messages := []openai.Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "hello", Files: []openai.File{{Name: "a.txt", Text: "text"}}, Images: [][]byte{{1, 2, 3}}},
	}
	client := New("sk-ant-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	ch := make(chan openai.StreamEvent)
	go client.StreamChat(context.Background(), "claude-test", messages, openai.Options{ReasoningEffort: "high"}, ch)
	var got string
	var usage openai.Usage
	for event := range ch {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		got += event.Delta
		if event.Done {
			usage = event.Usage
		}
	}
	if path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", path)
	}
	if got != "Hello Claude" {
		t.Errorf("text = %q", got)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	for _, want := range []string{
		`"model":"claude-test"`,
		`"system":[{"text":"Be concise.","type":"text"}]`,
		`"source":{"data":"AQID","media_type":"image/png","type":"base64"}`,
		`File: a.txt`,
		`"effort":"high"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request missing %s:\n%s", want, body)
		}
	}
}

func TestStreamAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`)
	}))
	defer srv.Close()
	client := New("bad", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	ch := make(chan openai.StreamEvent)
	go client.StreamChat(context.Background(), "claude-test", nil, openai.Options{}, ch)
	event := <-ch
	if event.Err == nil || event.Err.Error() != "anthropic: bad key" {
		t.Errorf("err = %v", event.Err)
	}
}
