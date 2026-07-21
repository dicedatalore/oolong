package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dicedatalore/oolong/internal/chat"
)

func TestStreamChat(t *testing.T) {
	var path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hello "},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"world"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":7,"eval_count":2}`)
	}))
	defer srv.Close()

	msgs := []chat.Message{{Role: "user", Content: "look", Files: []chat.File{{Name: "a.txt", Text: "text"}}, Images: [][]byte{{1, 2, 3}}}}
	ch := make(chan chat.StreamEvent)
	go New(srv.URL+"/v1").StreamChat(context.Background(), "gemma3", msgs, chat.Options{ReasoningEffort: "none"}, ch)
	var got string
	var usage chat.Usage
	for ev := range ch {
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		got += ev.Delta
		if ev.Done {
			usage = ev.Usage
		}
	}
	if path != "/api/chat" {
		t.Errorf("path = %q, want /api/chat", path)
	}
	if got != "hello world" || usage.InputTokens != 7 || usage.OutputTokens != 2 {
		t.Errorf("result = %q, usage %+v", got, usage)
	}
	for _, want := range []string{`"model":"gemma3"`, `"images":["AQID"]`, `File: a.txt`, `"think":false`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request missing %s: %s", want, body)
		}
	}
}

func TestStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()
	ch := make(chan chat.StreamEvent)
	go New(srv.URL).StreamChat(context.Background(), "missing", nil, chat.Options{}, ch)
	ev := <-ch
	if ev.Err == nil || ev.Err.Error() != "ollama: model not found" {
		t.Errorf("err = %v", ev.Err)
	}
}

func TestStreamChatCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan chat.StreamEvent)
	go New(srv.URL).StreamChat(ctx, "gemma3", nil, chat.Options{}, ch)
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
