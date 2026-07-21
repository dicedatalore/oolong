package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/chat"
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
		model, _ = model.Update(streamEventMsg{StreamEvent: ev, ch: ch})
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

func TestStaleStreamEventIgnoredAfterRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"old\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // hang until the client cancels
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	oldCh := model.(Model).stream
	ev := <-oldCh // received from the wire, but not yet applied by Update

	// esc stops the stream and ctrl+r immediately starts a new request, so
	// the old stream's queued event arrives while a new stream is in flight.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !model.(Model).waiting {
		t.Fatal("not waiting after regenerate")
	}

	model, _ = model.Update(streamEventMsg{StreamEvent: ev, ch: oldCh})
	am := model.(Model)
	if !am.waiting {
		t.Error("stale delta stopped the new request")
	}
	if last := am.messages[len(am.messages)-1]; last.Role != "user" {
		t.Errorf("stale delta appended a message: %+v", last)
	}

	// A stale done event must not cancel the new stream or book usage.
	model, _ = model.Update(streamEventMsg{
		StreamEvent: chat.StreamEvent{Done: true, Usage: chat.Usage{InputTokens: 9, OutputTokens: 9}},
		ch:          oldCh,
	})
	am = model.(Model)
	if !am.waiting {
		t.Error("stale done event stopped the new request")
	}
	if am.inputTokens != 0 || am.outputTokens != 0 {
		t.Errorf("stale usage booked: %d in / %d out", am.inputTokens, am.outputTokens)
	}
	am.finishStream() // cancel the in-flight stream so its goroutine exits
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
	if am.errText != "Request failed" || am.errorInfo == nil || am.errorInfo.detail != "openai: boom" {
		t.Errorf("error state = %q / %#v", am.errText, am.errorInfo)
	}
	if am.waiting {
		t.Error("still waiting after stream error")
	}
	if n := len(am.messages); n != 1 {
		t.Errorf("len(messages) = %d, want 1 (no assistant message on error)", n)
	}
}

func TestUsageAccountingKeepsReportedAndEstimatedValuesWithoutLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n")
	}))
	defer srv.Close()
	model := enterChat(t, srv)
	model = typeText(model, "hello")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)
	if header := model.(Model).chatHeader(); !strings.Contains(header, "7 in / 2 out") || strings.Contains(header, "reported") {
		t.Errorf("provider usage header = %q", header)
	}

	am := model.(Model)
	am.waiting = true
	am.estInputTokens = 12
	am.streaming = true
	am.messages = append(am.messages, chat.Message{Role: "assistant", Content: "estimated output"})
	ch := make(chan chat.StreamEvent)
	am.stream = ch
	model, _ = am.handleStreamEvent(streamEventMsg{StreamEvent: chat.StreamEvent{Done: true}, ch: ch})
	am = model.(Model)
	header := am.chatHeader()
	if !am.usageEstimated || strings.Contains(header, "estimated") || strings.Contains(header, "reported") {
		t.Errorf("estimated usage state or header is wrong: estimated=%v header=%q", am.usageEstimated, header)
	}
}
