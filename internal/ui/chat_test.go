package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/chat"
	"github.com/dicedatalore/oolong/internal/config"
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
	am.messages = []chat.Message{
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

func TestUpDownCyclesSentMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	am := model.(Model)
	am.messages = []chat.Message{
		{Role: "user", Content: "first", Images: [][]byte{{1}}},
		{Role: "assistant", Content: "ok", Model: am.chosen},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "ok", Model: am.chosen},
	}
	model = am
	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	value := func() string { return model.(Model).input.Value() }

	model, _ = model.Update(up)
	if value() != "second" {
		t.Fatalf("after ↑, composer = %q, want %q", value(), "second")
	}
	if imgs := model.(Model).pendingImages; imgs != nil {
		t.Errorf("recall of an image-less message left %d pending images", len(imgs))
	}

	model, _ = model.Update(up)
	if value() != "first" {
		t.Fatalf("after ↑↑, composer = %q, want %q", value(), "first")
	}
	if imgs := model.(Model).pendingImages; len(imgs) != 1 {
		t.Errorf("recalled message lost its attachment: %d pending images", len(imgs))
	}

	// Clamped at the oldest.
	model, _ = model.Update(up)
	if value() != "first" {
		t.Errorf("↑ past the oldest changed the composer to %q", value())
	}

	// ↓ steps back toward the newest, then restores the empty composer.
	model, _ = model.Update(down)
	if value() != "second" {
		t.Fatalf("after ↓, composer = %q, want %q", value(), "second")
	}
	model, _ = model.Update(down)
	am = model.(Model)
	if am.input.Value() != "" || am.pendingImages != nil || am.recallIdx != -1 {
		t.Errorf("↓ past the newest did not restore the composer: value=%q images=%d recallIdx=%d",
			am.input.Value(), len(am.pendingImages), am.recallIdx)
	}
}

func TestEditedRecallStopsCycling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := model.(Model).input.Value(); got != "hello" {
		t.Fatalf("recall = %q, want %q", got, "hello")
	}
	model = typeText(model, "!")
	if model.(Model).recallIdx != -1 {
		t.Error("editing the recall did not clear recall state")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := model.(Model).input.Value(); got != "hello!" {
		t.Errorf("↑ on an edited recall changed the composer to %q", got)
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

func TestCopyUsesTerminalClipboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	model := seedConversation(enterChat(t, srv))
	model, cmd := model.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("copy returned no OSC52 command")
	}
	if got := fmt.Sprint(cmd()); got != "hi" {
		t.Fatalf("clipboard payload = %q, want hi", got)
	}
	if got := model.(Model).chatNotice; got != "copied last reply" {
		t.Fatalf("chatNotice = %q", got)
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

func TestChatLayoutSurvivesCompactTerminalSizes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	model := seedConversation(enterChat(t, srv))
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 18}, {Width: 40, Height: 12}, {Width: 24, Height: 8}} {
		model, _ = model.Update(size)
		am := model.(Model)
		if am.vp.Width() < 1 || am.vp.Height() < 1 || am.input.Width() < 1 {
			t.Errorf("%dx%d produced invalid widget size: viewport=%dx%d input=%d", size.Width, size.Height, am.vp.Width(), am.vp.Height(), am.input.Width())
		}
		view := am.viewChat()
		if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
			t.Errorf("%dx%d chat rendered %dx%d", size.Width, size.Height, lipgloss.Width(view), lipgloss.Height(view))
		}
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

func TestConversationWidthCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	model, _ = model.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	am := model.(Model)
	if w := lipgloss.Width(am.msgCache[0]); w > maxMsgWidth {
		t.Errorf("message block width = %d on a 160-col window, want <= %d", w, maxMsgWidth)
	}
	if am.cacheWidth != maxMsgWidth {
		t.Errorf("cacheWidth = %d, want the %d cap", am.cacheWidth, maxMsgWidth)
	}

	// Both widths are past the cap, so the resize must not invalidate the
	// render cache.
	model, _ = model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if got := model.(Model).cacheWidth; got != maxMsgWidth {
		t.Errorf("cacheWidth after in-cap resize = %d, want %d", got, maxMsgWidth)
	}
}

func TestChatHeaderKeepsModelAndMetadataOnOneRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	am := seedConversation(enterChat(t, srv)).(Model)
	plain := ansi.ReplaceAllString(am.chatHeader(), "")
	lines := strings.Split(strings.TrimSpace(plain), "\n")
	if len(lines) != 1 {
		t.Fatalf("chat header has %d content lines, want 1: %q", len(lines), plain)
	}
	if !strings.Contains(lines[0], am.chosen) || !strings.Contains(lines[0], "in / 50 out") {
		t.Errorf("chat header does not keep model and metadata together: %q", plain)
	}
}

func TestChatComposerHasTopBoundary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	am := enterChat(t, srv).(Model)
	composer := ansi.ReplaceAllString(am.chatComposer(40), "")
	lines := strings.Split(composer, "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "─") {
		t.Errorf("composer has no top boundary: %q", composer)
	}
}

func TestMessageSpacingGroupsPromptWithReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	am := enterChat(t, srv).(Model)
	user := am.renderMessage(chat.Message{Role: "user", Content: "hello"})
	assistant := am.renderMessage(chat.Message{Role: "assistant", Content: "hi"})
	if !strings.HasSuffix(user, "\n\n") {
		t.Error("user prompt does not leave a slight gap before its reply")
	}
	if !strings.HasSuffix(assistant, "\n\n\n") {
		t.Error("assistant reply does not separate completed exchanges")
	}
}

func TestIncompleteCodeFenceStaysPlainWhileStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	am := enterChat(t, srv).(Model)
	content := "before\n```go\nfmt.Println(\"hi\")"
	plain := am.renderMessageMode(chat.Message{Role: "assistant", Content: content}, true)
	if !strings.Contains(plain, "```go") {
		t.Errorf("unfinished streaming fence was reformatted: %q", plain)
	}
	closed := am.renderMessageMode(chat.Message{Role: "assistant", Content: content + "\n```"}, true)
	if strings.Contains(closed, "```go") {
		t.Errorf("closed streaming fence was not rendered as Markdown: %q", closed)
	}
}

func TestAttachFilePickerToggle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model, cmd := model.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	am := model.(Model)
	if !am.pickingFile {
		t.Fatal("ctrl+f did not open the file picker")
	}
	if cmd == nil {
		t.Fatal("ctrl+f returned no init command for the picker")
	}
	if v := am.viewChat(); !strings.Contains(v, "Attach a file") {
		t.Error("view missing the picker title")
	}
	if h := lipgloss.Height(am.viewChat()); h != 24 {
		t.Errorf("picker view height = %d, want 24", h)
	}

	// esc cancels without touching the chat, and doesn't leave for the
	// model picker.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if am.pickingFile || am.state != stateChat {
		t.Errorf("esc: pickingFile=%v state=%v, want closed picker in chat", am.pickingFile, am.state)
	}
}

func TestAttachPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	textPath := write("main.go", []byte("package main\n"))
	pngPath := write("shot.png", []byte("\x89PNG\r\n\x1a\n00000000"))
	binPath := write("blob.bin", []byte{0x00, 0xFF, 0xFE, 0x00, 0x80, 0x81})

	am := enterChat(t, srv).(Model)
	am.attachPath(textPath)
	if len(am.pendingFiles) != 1 || am.pendingFiles[0].Name != "main.go" ||
		am.pendingFiles[0].Text != "package main\n" {
		t.Errorf("pendingFiles = %+v, want main.go with its content", am.pendingFiles)
	}
	am.attachPath(pngPath)
	if len(am.pendingImages) != 1 {
		t.Errorf("pendingImages = %d entries, want 1", len(am.pendingImages))
	}
	am.attachPath(binPath)
	if len(am.pendingFiles) != 1 || len(am.pendingImages) != 1 {
		t.Error("binary file was attached")
	}
	if !strings.Contains(am.chatNotice, "neither an image nor text") {
		t.Errorf("chatNotice = %q, want a not-attachable notice", am.chatNotice)
	}

	// Both attachments ride along on the next send, and the indicator line
	// names them.
	if label := am.attachmentLabel(); !strings.Contains(label, "image 1") || !strings.Contains(label, "main.go") {
		t.Errorf("attachmentLabel = %q, want image count and file name", label)
	}
	var model tea.Model = am
	model = typeText(model, "look")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am = model.(Model)
	sent := am.messages[len(am.messages)-1]
	if len(sent.Files) != 1 || len(sent.Images) != 1 {
		t.Errorf("sent message has %d files / %d images, want 1 / 1", len(sent.Files), len(sent.Images))
	}
	if am.pendingFiles != nil || am.pendingImages != nil {
		t.Error("pending attachments not cleared after send")
	}
	am.finishStream() // cancel the in-flight stream so its goroutine exits
}

func TestEditLastPromptRegeneratesAndRestoresDraft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	am := model.(Model)
	am.input.SetValue("unsent draft")
	am.pendingFiles = []chat.File{{Name: "draft.txt", Text: "draft attachment"}}
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	am = model.(Model)
	if am.editIndex != 0 || am.input.Value() != "hello" {
		t.Fatalf("edit state index=%d input=%q", am.editIndex, am.input.Value())
	}
	model = typeText(model, " edited")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am = model.(Model)
	if len(am.messages) != 1 || am.messages[0].Content != "hello edited" || !am.waiting {
		t.Errorf("edited conversation = %#v, waiting=%v", am.messages, am.waiting)
	}
	if am.input.Value() != "unsent draft" || len(am.pendingFiles) != 1 || am.pendingFiles[0].Name != "draft.txt" {
		t.Errorf("draft not restored: input=%q files=%#v", am.input.Value(), am.pendingFiles)
	}
	am.finishStream()
}

func TestEditLastPromptCanBeCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := seedConversation(enterChat(t, srv))
	am := model.(Model)
	am.input.SetValue("draft")
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if am.state != stateChat || am.editIndex >= 0 || am.input.Value() != "draft" || len(am.messages) != 2 {
		t.Errorf("cancel edit state=%v index=%d input=%q messages=%d", am.state, am.editIndex, am.input.Value(), len(am.messages))
	}
}

func TestRetryLastResponseWithAnotherModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cfg := config.Config{Provider: "openai", Models: []config.Model{{ID: "small"}, {ID: "large"}}}
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	am.messages = []chat.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "old", Model: "small"}}
	am.input.SetValue("draft")
	am.layoutChat()
	model = am

	model, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	am = model.(Model)
	if am.state != statePicker || !am.retryModel {
		t.Fatalf("retry picker state=%v retry=%v", am.state, am.retryModel)
	}
	am.selectModel("large")
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am = model.(Model)
	if am.state != stateChat || am.chosen != "large" || !am.waiting || len(am.messages) != 1 {
		t.Errorf("retry result state=%v model=%q waiting=%v messages=%#v", am.state, am.chosen, am.waiting, am.messages)
	}
	if am.input.Value() != "draft" {
		t.Errorf("retry lost draft: %q", am.input.Value())
	}
	am.finishStream()
}

func TestPendingAttachmentsCanBeRemovedAndCleared(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	am := enterChat(t, srv).(Model)
	am.pendingImages = [][]byte{{1}, {2}}
	am.pendingFiles = []chat.File{{Name: "one.txt"}, {Name: "two.txt"}}
	am.layoutChat()
	if view := am.chatComposer(60); !strings.Contains(view, "image 1") || !strings.Contains(view, "image 2") || !strings.Contains(view, "one.txt") || !strings.Contains(view, "two.txt") {
		t.Errorf("composer does not list attachments individually: %q", view)
	}
	var model tea.Model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	am = model.(Model)
	if len(am.pendingFiles) != 1 || am.pendingFiles[0].Name != "one.txt" {
		t.Errorf("remove-last left files %#v", am.pendingFiles)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt})
	am = model.(Model)
	if len(am.pendingImages) != 0 || len(am.pendingFiles) != 0 {
		t.Errorf("clear-all left %d images and %d files", len(am.pendingImages), len(am.pendingFiles))
	}
}

func TestNewOutputIndicator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	am := enterChat(t, srv).(Model)
	for i := range 8 {
		am.messages = append(am.messages,
			chat.Message{Role: "user", Content: fmt.Sprintf("question %d\n%s", i, strings.Repeat("line\n", 4))},
			chat.Message{Role: "assistant", Content: fmt.Sprintf("answer %d\n%s", i, strings.Repeat("reply\n", 4))})
	}
	am.layoutChat()
	am.vp.GotoTop()
	am.vp.GotoTop()
	am.waiting = true
	am.streaming = false
	ch := make(chan chat.StreamEvent)
	am.stream = ch
	model, _ := am.handleStreamEvent(streamEventMsg{StreamEvent: chat.StreamEvent{Delta: "new"}, ch: ch})
	am = model.(Model)
	if !am.newOutputBelow || !strings.Contains(am.viewChat(), "new output below") {
		t.Error("streaming while scrolled up did not show the new-output indicator")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	am = model.(Model)
	if am.newOutputBelow {
		t.Error("end did not clear the new-output indicator")
	}
	am.finishStream()
}

func TestContextPreflightKeepsDraftAndRequiresAChoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cfg := config.Config{Provider: "openai", Models: []config.Model{{ID: "tiny", ContextWindow: 10}}}
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = typeText(model, strings.Repeat("x", 40))
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if !am.contextWarning || am.waiting || len(am.messages) != 0 {
		t.Fatalf("preflight warning=%v waiting=%v messages=%d", am.contextWarning, am.waiting, len(am.messages))
	}
	if !strings.Contains(am.viewChat(), "send anyway") || !strings.Contains(am.viewChat(), "drop oldest") {
		t.Error("context warning does not show recovery choices")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if am.contextWarning || am.input.Value() != strings.Repeat("x", 40) {
		t.Errorf("cancel lost draft: warning=%v input=%q", am.contextWarning, am.input.Value())
	}

	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	am = model.(Model)
	if am.contextWarning || !am.waiting || len(am.messages) != 1 {
		t.Errorf("send-anyway warning=%v waiting=%v messages=%d", am.contextWarning, am.waiting, len(am.messages))
	}
	am.finishStream()
}

func TestContextPreflightCanRemoveAttachmentsAndOldestTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cfg := config.Config{Provider: "openai", Models: []config.Model{{ID: "tiny", ContextWindow: 100}}}
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	am.messages = []chat.Message{
		{Role: "user", Content: strings.Repeat("old", 100)},
		{Role: "assistant", Content: strings.Repeat("reply", 100)},
		{Role: "user", Content: "newer"},
		{Role: "assistant", Content: "response"},
	}
	am.pendingImages = [][]byte{{1, 2, 3}}
	am.input.SetValue("next")
	am.layoutChat()
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).contextWarning {
		t.Fatal("large history did not trigger preflight")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	am = model.(Model)
	if len(am.pendingImages) != 0 || len(am.contextPending.Images) != 0 {
		t.Error("remove-attachments choice kept image data")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	am = model.(Model)
	if len(am.messages) != 2 || am.messages[0].Content != "newer" {
		t.Errorf("drop-oldest left messages %#v", am.messages)
	}
}

func TestRegenerateUsesContextPreflightBeforeDroppingReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cfg := config.Config{Provider: "openai", Models: []config.Model{{ID: "tiny", ContextWindow: 10}}}
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	am.messages = []chat.Message{
		{Role: "user", Content: strings.Repeat("x", 40)},
		{Role: "assistant", Content: "keep until confirmed"},
	}
	am.layoutChat()
	model = am
	model, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	am = model.(Model)
	if !am.contextWarning || !am.contextResend || len(am.messages) != 2 || am.waiting {
		t.Fatalf("regen preflight warning=%v resend=%v messages=%d waiting=%v", am.contextWarning, am.contextResend, len(am.messages), am.waiting)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am = model.(Model)
	if len(am.messages) != 2 || am.messages[1].Content != "keep until confirmed" {
		t.Error("cancelling regenerate discarded the existing reply")
	}
}

func TestErrorDetailsToggle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	am := enterChat(t, srv).(Model)
	am.messages = []chat.Message{{Role: "user", Content: "hello"}}
	am.waiting = true
	am.estInputTokens = 2
	ch := make(chan chat.StreamEvent)
	am.stream = ch
	var model tea.Model
	model, _ = am.handleStreamEvent(streamEventMsg{StreamEvent: chat.StreamEvent{Err: errors.New("openai: HTTP 401 secret detail")}, ch: ch})
	am = model.(Model)
	if !strings.Contains(am.viewChat(), "Authentication failed") || strings.Contains(am.viewChat(), "secret detail") {
		t.Error("error view did not hide technical detail initially")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModCtrl})
	am = model.(Model)
	if !strings.Contains(am.viewChat(), "secret detail") {
		t.Error("ctrl+i did not reveal error detail")
	}
}

func TestContextMeterInHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// The built-in catalog knows the window size (400k tokens).
	model := enterChat(t, srv)
	am := model.(Model)
	if v := am.viewChat(); !strings.Contains(v, "context 0%") {
		t.Error("header missing the context meter for a built-in model")
	}

	// 1.28M chars ≈ 320k tokens = 80% of the window: the meter becomes a
	// warning.
	am.messages = []chat.Message{{Role: "user", Content: strings.Repeat("a", 1_280_000)}}
	if v := am.viewChat(); !strings.Contains(v, "context 80% full") {
		t.Error("header missing the near-full warning at 80%")
	}

	// A model with no known window shows no meter.
	cfg := config.Config{
		DefaultModel: "mystery",
		Models:       []config.Model{{ID: "mystery", Provider: "openai"}},
	}
	model = newCustomModel(t, srv, cfg)
	if v := model.(Model).viewChat(); strings.Contains(v, "context ") {
		t.Error("header shows a context meter without a window size")
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
