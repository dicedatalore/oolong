package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestUpDownCyclesSentMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	am := model.(Model)
	am.messages = []openai.Message{
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

func TestEditorRequiresEditorEnv(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model, cmd := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Error("ctrl+e without $EDITOR returned a command")
	}
	if got := model.(Model).chatNotice; !strings.Contains(got, "$EDITOR") {
		t.Errorf("chatNotice = %q, want a $EDITOR hint", got)
	}
}

func TestEditorRoundTrip(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true") // exits 0 without touching the file
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	model = typeText(model, "draft")
	model, cmd := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+e with $EDITOR set returned no command")
	}

	// Stand in for the editor: overwrite the temp file, then deliver the
	// finished message the ExecProcess callback would produce.
	f, err := os.CreateTemp(t.TempDir(), "edited-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("composed in editor\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	model, _ = model.Update(editorFinishedMsg{path: f.Name()})
	if got := model.(Model).input.Value(); got != "composed in editor" {
		t.Errorf("composer after editor = %q, want %q", got, "composed in editor")
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Error("temp file not cleaned up after the editor round trip")
	}

	// A failed editor leaves the composer alone.
	model, _ = model.Update(editorFinishedMsg{path: "does-not-exist", err: fmt.Errorf("exit 1")})
	am := model.(Model)
	if am.input.Value() != "composed in editor" {
		t.Errorf("failed editor changed the composer: %q", am.input.Value())
	}
	if !strings.Contains(am.errText, "exit 1") {
		t.Errorf("errText = %q, want the editor error", am.errText)
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
	if label := am.attachmentLabel(); !strings.Contains(label, "1 image") || !strings.Contains(label, "main.go") {
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

func TestContextMeterInHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// The built-in catalog knows the window size (400k tokens).
	model := enterChat(t, srv)
	am := model.(Model)
	if v := am.viewChat(); !strings.Contains(v, "ctx 0%") {
		t.Error("header missing the context meter for a built-in model")
	}

	// 1.28M chars ≈ 320k tokens = 80% of the window: the meter becomes a
	// warning.
	am.messages = []openai.Message{{Role: "user", Content: strings.Repeat("a", 1_280_000)}}
	if v := am.viewChat(); !strings.Contains(v, "ctx 80% full") {
		t.Error("header missing the near-full warning at 80%")
	}

	// A model with no known window shows no meter.
	cfg := config.Config{
		DefaultModel: "mystery",
		Models:       []config.Model{{ID: "mystery"}},
	}
	model = newCustomModel(t, srv, cfg)
	if v := model.(Model).viewChat(); strings.Contains(v, "ctx ") {
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
