package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/option"
)

func clientFor(srv *httptest.Server) *Client {
	return New("test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
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

	ch := make(chan StreamEvent)
	go clientFor(srv).StreamChat(context.Background(), "m", nil, ch)

	var got string
	var deltas int
	for ev := range ch {
		switch {
		case ev.Err != nil:
			t.Fatalf("unexpected error: %v", ev.Err)
		case ev.Done:
			if ev.Usage.InputTokens != 5 || ev.Usage.OutputTokens != 3 {
				t.Errorf("usage = %+v, want 5/3", ev.Usage)
			}
		default:
			deltas++
			got += ev.Delta
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

	ch := make(chan StreamEvent)
	go clientFor(srv).StreamChat(context.Background(), "m", nil, ch)

	ev := <-ch
	if ev.Err == nil || ev.Err.Error() != "openai: bad key" {
		t.Errorf("err = %v, want openai: bad key", ev.Err)
	}
	if _, ok := <-ch; ok {
		t.Error("channel not closed after error")
	}
}

func TestStreamChatSendsImages(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		sseEvent(w, "response.completed",
			`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer srv.Close()

	msgs := []Message{{Role: "user", Content: "look", Images: [][]byte{{1, 2, 3}}}}
	ch := make(chan StreamEvent)
	go clientFor(srv).StreamChat(context.Background(), "m", msgs, ch)
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("unexpected error: %v", ev.Err)
		}
	}

	for _, want := range []string{
		`"text":"look"`,
		`"image_url":"data:image/png;base64,AQID"`, // base64 of 1,2,3
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request body missing %s:\n%s", want, body)
		}
	}
}

func TestStreamChatFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseEvent(w, "response.failed",
			`{"type":"response.failed","response":{"error":{"code":"server_error","message":"boom"}}}`)
	}))
	defer srv.Close()

	ch := make(chan StreamEvent)
	go clientFor(srv).StreamChat(context.Background(), "m", nil, ch)

	ev := <-ch
	if ev.Err == nil || ev.Err.Error() != "openai: boom" {
		t.Errorf("err = %v, want openai: boom", ev.Err)
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
	ch := make(chan StreamEvent)
	done := make(chan struct{})
	go func() {
		clientFor(srv).StreamChat(ctx, "m", nil, ch)
		close(done)
	}()

	if ev := <-ch; ev.Delta != "partial" {
		t.Fatalf("delta = %q, want partial", ev.Delta)
	}
	cancel() // nobody reads ch anymore; goroutine must still exit
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamChat goroutine leaked after cancel")
	}
}
