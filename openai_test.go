package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/option"
)

func clientFor(srv *httptest.Server) *openaiClient {
	return newOpenAIClient("test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

func sseEvent(w http.ResponseWriter, typ, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data)
}

func TestStreamChatDeltasAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, s := range []string{"Hello", ", ", "world"} {
			sseEvent(w, "response.output_text.delta",
				fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, s))
			fl.Flush()
		}
		sseEvent(w, "response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`)
	}))
	defer srv.Close()

	ch := make(chan streamEvent)
	go clientFor(srv).streamChat(context.Background(), "m", nil, ch)

	var got string
	var deltas int
	for ev := range ch {
		switch {
		case ev.err != nil:
			t.Fatalf("unexpected error: %v", ev.err)
		case ev.done:
			if ev.usage.InputTokens != 5 || ev.usage.OutputTokens != 3 {
				t.Errorf("usage = %+v, want 5/3", ev.usage)
			}
		default:
			deltas++
			got += ev.delta
		}
	}
	if got != "Hello, world" || deltas != 3 {
		t.Errorf("got %q in %d deltas, want \"Hello, world\" in 3", got, deltas)
	}
}

func TestStreamChatAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()

	ch := make(chan streamEvent)
	go clientFor(srv).streamChat(context.Background(), "m", nil, ch)

	ev := <-ch
	if ev.err == nil || ev.err.Error() != "openai: bad key" {
		t.Errorf("err = %v, want openai: bad key", ev.err)
	}
	if _, ok := <-ch; ok {
		t.Error("channel not closed after error")
	}
}

func TestStreamChatFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseEvent(w, "response.failed",
			`{"type":"response.failed","response":{"error":{"code":"server_error","message":"boom"}}}`)
	}))
	defer srv.Close()

	ch := make(chan streamEvent)
	go clientFor(srv).streamChat(context.Background(), "m", nil, ch)

	ev := <-ch
	if ev.err == nil || ev.err.Error() != "openai: boom" {
		t.Errorf("err = %v, want openai: boom", ev.err)
	}
	if _, ok := <-ch; ok {
		t.Error("channel not closed after error")
	}
}

func TestStreamChatCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		sseEvent(w, "response.output_text.delta",
			`{"type":"response.output_text.delta","delta":"partial"}`)
		fl.Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan streamEvent)
	done := make(chan struct{})
	go func() {
		clientFor(srv).streamChat(ctx, "m", nil, ch)
		close(done)
	}()

	if ev := <-ch; ev.delta != "partial" {
		t.Fatalf("delta = %q, want partial", ev.delta)
	}
	cancel() // nobody reads ch anymore; goroutine must still exit
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamChat goroutine leaked after cancel")
	}
}
