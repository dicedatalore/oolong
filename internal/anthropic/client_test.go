package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/dicedatalore/oolong/internal/chat"
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

	messages := []chat.Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "hello", Files: []chat.File{{Name: "a.txt", Text: "text"}}, Images: [][]byte{{1, 2, 3}}},
	}
	client := New("sk-ant-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	ch := make(chan chat.StreamEvent)
	go client.StreamChat(context.Background(), "claude-test", messages, chat.Options{ReasoningEffort: "high"}, ch)
	var got string
	var usage chat.Usage
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
	ch := make(chan chat.StreamEvent)
	go client.StreamChat(context.Background(), "claude-test", nil, chat.Options{}, ch)
	event := <-ch
	if event.Err == nil || event.Err.Error() != "anthropic: bad key" {
		t.Errorf("err = %v", event.Err)
	}
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"valid", http.StatusOK, `{"data":[],"has_more":false}`, ""},
		{"unauthorized hides key", http.StatusUnauthorized, `{"type":"error","error":{"message":"bad sk-ant-secret"}}`, "invalid Anthropic API key"},
		{"server error", http.StatusInternalServerError, `{"type":"error","error":{"message":"down"}}`, "anthropic: down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			err := validateKey("sk-ant-secret", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
			if tt.want == "" && err != nil {
				t.Fatalf("validateKey() error = %v", err)
			}
			if tt.want != "" && (err == nil || err.Error() != tt.want) {
				t.Fatalf("validateKey() error = %v, want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "sk-ant-secret") {
				t.Fatalf("validateKey() exposed credential: %v", err)
			}
		})
	}
}

func TestStreamChatCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan chat.StreamEvent)
	go New("test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)).StreamChat(ctx, "claude-test", nil, chat.Options{}, ch)
	<-started
	cancel()
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("event after cancellation = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}
