package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pumpStream feeds every event from the in-flight stream through Update,
// standing in for the readStream command loop the Bubble Tea runtime drives.
func pumpStream(t *testing.T, model tea.Model) tea.Model {
	t.Helper()
	ch := model.(Model).stream
	if ch == nil {
		t.Fatal("no stream in flight")
	}
	for ev := range ch {
		model, _ = model.Update(streamEventMsg(ev))
	}
	return model
}

func TestStreamDeltasAccumulateAndDoneRecordsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, s := range []string{"Hello", " world"} {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", s)
			fl.Flush()
		}
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2,\"total_tokens\":9}}}\n\n")
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)

	am := model.(Model)
	if am.waiting {
		t.Error("still waiting after done event")
	}
	last := am.messages[len(am.messages)-1]
	if last.Role != "assistant" || last.Content != "Hello world" {
		t.Errorf("assistant message = %+v, want %q", last, "Hello world")
	}
	if am.inputTokens != 7 || am.outputTokens != 2 {
		t.Errorf("tokens = %d in / %d out, want 7 in / 2 out", am.inputTokens, am.outputTokens)
	}
	if am.errText != "" {
		t.Errorf("errText = %q, want empty", am.errText)
	}
}

func TestStreamErrorShowsMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"boom\"}}}\n\n")
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)

	am := model.(Model)
	if am.errText != "openai: boom" {
		t.Errorf("errText = %q, want %q", am.errText, "openai: boom")
	}
	if am.waiting {
		t.Error("still waiting after stream error")
	}
	if n := len(am.messages); n != 1 {
		t.Errorf("len(messages) = %d, want 1 (no assistant message on error)", n)
	}
}
