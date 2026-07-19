package google

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dicedatalore/oolong/internal/openai"
)

func chunk(w io.Writer, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func TestStreamChat(t *testing.T) {
	var path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk(w, `{"candidates":[{"content":{"parts":[{"text":"Hello "}],"role":"model"}}],"usageMetadata":{"promptTokenCount":7}}`)
		chunk(w, `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true},{"text":"Gemini"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"thoughtsTokenCount":1}}`)
	}))
	defer srv.Close()

	messages := []openai.Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "hello", Files: []openai.File{{Name: "a.txt", Text: "text"}}, Images: [][]byte{{1, 2, 3}}},
	}
	client := New("test-key", WithBaseURL(srv.URL))
	ch := make(chan openai.StreamEvent)
	go client.StreamChat(context.Background(), "gemini-test", messages, openai.Options{ReasoningEffort: "high"}, ch)
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
	if want := "/v1beta/models/gemini-test:streamGenerateContent"; path != want {
		t.Errorf("path = %q, want %s", path, want)
	}
	if got != "Hello Gemini" {
		t.Errorf("text = %q", got)
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want thought tokens counted as output", usage)
	}
	for _, want := range []string{
		`"text":"Be concise."`,
		`"text":"hello"`,
		`"data":"AQID"`,
		`"mimeType":"image/png"`,
		`File: a.txt`,
		`"thinkingLevel":"HIGH"`,
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
		fmt.Fprint(w, `{"error":{"code":401,"message":"bad key","status":"UNAUTHENTICATED"}}`)
	}))
	defer srv.Close()
	client := New("bad", WithBaseURL(srv.URL))
	ch := make(chan openai.StreamEvent)
	go client.StreamChat(context.Background(), "gemini-test", nil, openai.Options{}, ch)
	event := <-ch
	if event.Err == nil || event.Err.Error() != "google: bad key" {
		t.Errorf("err = %v", event.Err)
	}
}

func TestThinkingLevel(t *testing.T) {
	for effort, want := range map[string]string{
		"":       "",
		"none":   "MINIMAL",
		"low":    "LOW",
		"medium": "MEDIUM",
		"high":   "HIGH",
		"xhigh":  "HIGH",
		"custom": "CUSTOM",
	} {
		if got := string(thinkingLevel(effort)); got != want {
			t.Errorf("thinkingLevel(%q) = %q, want %q", effort, got, want)
		}
	}
}
