package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/chat"
)

func TestSaveTranscript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	t.Chdir(t.TempDir())

	model := enterChat(t, srv)
	am := model.(Model)
	am.systemPrompt = "be brief"
	am.messages = []chat.Message{
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
	if strings.Contains(md, "<!--") {
		t.Errorf("transcript contains hidden metadata:\n%s", md)
	}
}

func TestSaveTranscriptHonorsDirEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("OOLONG_TRANSCRIPT_DIR", dir)

	model := enterChat(t, srv)
	am := model.(Model)
	am.messages = []chat.Message{{Role: "user", Content: "hello"}}
	// The env var wins even when the config also names a directory.
	am.transcriptDir = t.TempDir()
	model = am

	model, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	am = model.(Model)
	path := strings.TrimPrefix(am.chatNotice, "saved ")
	if filepath.Dir(path) != dir {
		t.Errorf("transcript saved to %q, want directory %q", path, dir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestSaveTranscriptUsesConfigDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()

	model := enterChat(t, srv)
	am := model.(Model)
	am.messages = []chat.Message{{Role: "user", Content: "hello"}}
	am.transcriptDir = dir
	model = am

	model, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	am = model.(Model)
	path := strings.TrimPrefix(am.chatNotice, "saved ")
	if filepath.Dir(path) != dir {
		t.Errorf("transcript saved to %q, want config directory %q", path, dir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
