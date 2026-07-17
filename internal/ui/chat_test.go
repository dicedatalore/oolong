package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/openai"
)

func TestChatMultilineInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	var model tea.Model = New(clientFor(srv), "dark")
	step := func(msg tea.Msg) {
		model, _ = model.Update(msg)
	}

	step(tea.WindowSizeMsg{Width: 80, Height: 24})
	step(tea.KeyPressMsg{Code: tea.KeyEnter}) // pick the first model

	am := model.(Model)
	if am.state != stateChat {
		t.Fatalf("state = %v, want stateChat", am.state)
	}

	for _, r := range "hi" {
		step(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	step(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	step(tea.KeyPressMsg{Code: 'y', Text: "y"})

	am = model.(Model)
	if got := am.input.Value(); got != "hi\ny" {
		t.Fatalf("input after shift+enter = %q, want \"hi\\ny\"", got)
	}
	if am.input.Height() < 2 {
		t.Errorf("textarea height = %d, want >= 2 after newline", am.input.Height())
	}

	step(tea.KeyPressMsg{Code: tea.KeyEnter}) // plain enter sends
	am = model.(Model)
	if !am.waiting {
		t.Error("not waiting after send")
	}
	if am.input.Value() != "" {
		t.Errorf("input not cleared after send: %q", am.input.Value())
	}
	last := am.messages[len(am.messages)-1]
	if last.Role != "user" || last.Content != "hi\ny" {
		t.Errorf("sent message = %+v, want user %q", last, "hi\ny")
	}
	am.finishStream() // cancel the in-flight stream so its goroutine exits
}

func TestStopStreamKeysStayInChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client cancels
	}))
	defer srv.Close()

	for _, k := range []string{"esc", "ctrl+c"} {
		model := enterChat(t, srv)
		model = typeText(model, "hi")
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !model.(Model).waiting {
			t.Fatalf("%s: not waiting after send", k)
		}

		key := tea.KeyPressMsg{Code: tea.KeyEscape}
		if k == "ctrl+c" {
			key = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		}
		model, cmd := model.Update(key)
		am := model.(Model)
		if am.state != stateChat {
			t.Errorf("%s while streaming left chat state", k)
		}
		if am.waiting {
			t.Errorf("%s while streaming did not stop the stream", k)
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Errorf("%s while streaming quit the program", k)
			}
		}
	}
}

func TestSystemPromptEditing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "draft msg")
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	am := model.(Model)
	if !am.editingSystem {
		t.Fatal("ctrl+p did not enter system prompt editing")
	}
	if am.input.Value() != "" {
		t.Errorf("system prompt editor not empty initially: %q", am.input.Value())
	}

	model = typeText(model, "be brief")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am = model.(Model)
	if am.editingSystem {
		t.Error("enter did not leave system prompt editing")
	}
	if am.systemPrompt != "be brief" {
		t.Errorf("systemPrompt = %q, want %q", am.systemPrompt, "be brief")
	}
	if am.input.Value() != "draft msg" {
		t.Errorf("draft not restored: %q", am.input.Value())
	}

	// esc cancels without touching the saved prompt.
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if got := model.(Model).input.Value(); got != "be brief" {
		t.Errorf("editor did not load saved prompt: %q", got)
	}
	model = typeText(model, " and rude")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if am.editingSystem || am.systemPrompt != "be brief" {
		t.Errorf("esc changed prompt: editing=%v prompt=%q", am.editingSystem, am.systemPrompt)
	}
	if am.input.Value() != "draft msg" {
		t.Errorf("draft not restored after cancel: %q", am.input.Value())
	}
}

func TestEscLeavesChatAndResetsSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	am := model.(Model)
	am.messages = []openai.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	am.inputTokens, am.outputTokens = 100, 50
	am.errText = "old error"
	am.systemPrompt = "be brief"
	model = am

	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := model.(Model).state; got != statePicker {
		t.Fatalf("state after esc = %v, want statePicker", got)
	}

	// Picking a model again starts a fresh session.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am = model.(Model)
	if am.state != stateChat {
		t.Fatal("not back in chat after picking a model")
	}
	if len(am.messages) != 0 || am.inputTokens != 0 || am.outputTokens != 0 || am.errText != "" {
		t.Errorf("session not reset: messages=%d in=%d out=%d err=%q",
			len(am.messages), am.inputTokens, am.outputTokens, am.errText)
	}
	if am.systemPrompt != "" {
		t.Errorf("system prompt survived the reset: %q", am.systemPrompt)
	}
}

func TestConversationViewCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, s := range []string{"Hello", " world"} {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", s)
			fl.Flush()
		}
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)

	am := model.(Model)
	if len(am.msgCache) != len(am.messages) {
		t.Fatalf("cache has %d entries for %d messages", len(am.msgCache), len(am.messages))
	}
	cached := am.conversationView()
	am.msgCache = nil
	if fresh := am.conversationView(); cached != fresh {
		t.Error("cached view differs from a fresh render")
	}

	// Resizing changes the render width, so cached blocks must be rebuilt.
	before := am.msgCache[0]
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	am = model.(Model)
	if am.msgCache[0] == before {
		t.Error("resize did not re-render cached messages")
	}

	// Leaving the chat drops the cache with the transcript.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := model.(Model).msgCache; got != nil {
		t.Errorf("cache survived leaving the chat: %d entries", len(got))
	}
}

func TestHelpToggle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model, _ = model.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	am := model.(Model)
	if !am.help.ShowAll {
		t.Error("? with empty input did not expand help")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if model.(Model).help.ShowAll {
		t.Error("second ? did not collapse help")
	}

	// With text in the input, ? is just a character.
	model = typeText(model, "what")
	model, _ = model.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	am = model.(Model)
	if am.help.ShowAll {
		t.Error("? while typing expanded help")
	}
	if am.input.Value() != "what?" {
		t.Errorf("input = %q, want %q", am.input.Value(), "what?")
	}
}
