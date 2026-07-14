package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type rewriteTransport struct{ base string }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func clientFor(srv *httptest.Server) *openaiClient {
	return &openaiClient{apiKey: "test", http: &http.Client{Transport: rewriteTransport{srv.URL}}}
}

func TestStreamChatDeltasAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, s := range []string{"Hello", ", ", "world"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", s)
			fl.Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
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
			if ev.usage.PromptTokens != 5 || ev.usage.CompletionTokens != 3 {
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

func TestStreamChatCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
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
