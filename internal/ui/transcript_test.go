package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/openai"
)

func TestSaveTranscript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	t.Chdir(t.TempDir())

	model := enterChat(t, srv)
	am := model.(Model)
	am.systemPrompt = "be brief"
	am.messages = []openai.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	model = am

	model, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	am = model.(Model)
	if !strings.HasPrefix(am.chatNotice, "saved ") {
		t.Fatalf("chatNotice = %q, want saved <file>", am.chatNotice)
	}
	data, err := os.ReadFile(strings.TrimPrefix(am.chatNotice, "saved "))
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)
	for _, want := range []string{"## You", "hello", "hi there", "be brief", am.chosen} {
		if !strings.Contains(md, want) {
			t.Errorf("transcript missing %q:\n%s", want, md)
		}
	}
}
