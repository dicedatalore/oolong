package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/openai"
)

func TestChatMultilineInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	var model tea.Model = New(clientFor(srv), "dark", config.Config{}, "")
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

func TestEscStopsStreamAndStaysInChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client cancels
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).waiting {
		t.Fatal("not waiting after send")
	}

	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am := model.(Model)
	if am.state != stateChat {
		t.Error("esc while streaming left chat state")
	}
	if am.waiting {
		t.Error("esc while streaming did not stop the stream")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("esc while streaming quit the program")
		}
	}
}

func TestCtrlCQuitsEvenWhileStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client cancels
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).waiting {
		t.Fatal("not waiting after send")
	}

	model, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c while streaming did not quit")
	}
	am := model.(Model)
	am.finishStream() // cancel the in-flight stream so its goroutine exits
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

// seedConversation puts a small finished conversation on the model.
func seedConversation(model tea.Model) tea.Model {
	am := model.(Model)
	am.messages = []openai.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", Model: am.chosen},
	}
	am.inputTokens, am.outputTokens = 100, 50
	am.systemPrompt = "be brief"
	return am
}

func TestEscKeepsChatForModelSwitch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := model.(Model).state; got != statePicker {
		t.Fatalf("state after esc = %v, want statePicker", got)
	}

	// Picking a model continues the same conversation.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if am.state != stateChat {
		t.Fatal("not back in chat after picking a model")
	}
	if len(am.messages) != 2 || am.systemPrompt != "be brief" {
		t.Errorf("conversation lost on model switch: messages=%d prompt=%q",
			len(am.messages), am.systemPrompt)
	}
	if am.inputTokens != 100 || am.outputTokens != 50 {
		t.Errorf("token totals reset on model switch: %d in / %d out", am.inputTokens, am.outputTokens)
	}
}

func TestPickerEscQuitsEvenWithKeptChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc on picker returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("esc on picker did not quit")
	}
}

func TestCtrlNStartsFresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	am := model.(Model)
	if am.state != stateChat {
		t.Fatal("ctrl+n left the chat screen")
	}
	if len(am.messages) != 0 || am.inputTokens != 0 || am.outputTokens != 0 || am.systemPrompt != "" {
		t.Errorf("session not reset: messages=%d in=%d out=%d prompt=%q",
			len(am.messages), am.inputTokens, am.outputTokens, am.systemPrompt)
	}
	if am.chatNotice != "new chat" {
		t.Errorf("chatNotice = %q, want %q", am.chatNotice, "new chat")
	}
}

func TestRegenerateReplacesLastReply(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"reply %d\"}\n\n", calls)
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)

	model, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !model.(Model).waiting {
		t.Fatal("ctrl+r did not start a new request")
	}
	model = pumpStream(t, model)

	am := model.(Model)
	if n := len(am.messages); n != 2 {
		t.Fatalf("len(messages) = %d, want 2 (user + regenerated reply)", n)
	}
	if got := am.messages[1].Content; got != "reply 2" {
		t.Errorf("regenerated reply = %q, want %q", got, "reply 2")
	}
}

func TestUpRecallsLastMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := model.(Model).input.Value(); got != "hello" {
		t.Errorf("input after up = %q, want %q", got, "hello")
	}

	// With text in the composer, up is cursor movement, not recall.
	am := model.(Model)
	am.input.SetValue("draft")
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := model.(Model).input.Value(); got != "draft" {
		t.Errorf("up clobbered a non-empty composer: %q", got)
	}
}

func TestEscMidStreamCountsEstimatedUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"twelve chars\"}\n\n")
		fl.Flush()
		<-r.Context().Done() // never completes, so no usage report
	}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "hello there mate") // 16 chars → 4 tokens estimated
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	ch := model.(Model).stream
	ev := <-ch
	model, _ = model.Update(streamEventMsg{StreamEvent: ev, ch: ch}) // "twelve chars" → 3 tokens estimated

	// While streaming, the header shows the live estimate.
	am := model.(Model)
	if v := am.viewChat(); !strings.Contains(v, "4 in / 3 out") {
		t.Errorf("streaming header missing live estimate 4 in / 3 out:\n%s", v)
	}

	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if am.waiting {
		t.Fatal("still waiting after esc")
	}
	if am.inputTokens != 4 || am.outputTokens != 3 {
		t.Errorf("settled tokens = %d in / %d out, want 4 in / 3 out (estimated)",
			am.inputTokens, am.outputTokens)
	}
	if am.costUSD == 0 {
		t.Error("costUSD not updated for a stopped stream")
	}
}

func TestCopyWithNothingToCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model, _ = model.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if got := model.(Model).chatNotice; got != "nothing to copy yet" {
		t.Errorf("chatNotice = %q, want %q", got, "nothing to copy yet")
	}
}

func TestChatViewFillsWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// Fresh chat: two lines of content must not let the input and bottom
	// bar float up under it.
	model := enterChat(t, srv) // 80x24 window
	am := model.(Model)
	if h := lipgloss.Height(am.viewChat()); h != 24 {
		t.Errorf("empty chat view height = %d, want 24", h)
	}

	model = seedConversation(model)
	am = model.(Model)
	am.layoutChat()
	if h := lipgloss.Height(am.viewChat()); h != 24 {
		t.Errorf("seeded chat view height = %d, want 24", h)
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

	// esc keeps the transcript (and its cache); ctrl+n on the picker
	// discards both.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := model.(Model).msgCache; got == nil {
		t.Error("esc dropped the cache along with the kept chat")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if got := model.(Model).msgCache; got != nil {
		t.Errorf("cache survived ctrl+n: %d entries", len(got))
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
