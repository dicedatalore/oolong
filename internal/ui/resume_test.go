package ui

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/openai"
)

func TestTranscriptRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	am := model.(Model)
	am.systemPrompt = "be brief\nand kind"
	am.messages = []openai.Message{
		{Role: "user", Content: "show me a heading"},
		{Role: "assistant", Model: "model --> arbitrary", Content: "## Sure\n\n```md\n## fenced heading\n```\n\n<!--oolong: exact -->"},
		{Role: "user", Content: "thanks", Images: [][]byte{{1, 2, 3}}, Files: []openai.File{{Name: "a.txt", Text: "contents"}}},
	}

	got, err := parseTranscript(am.transcriptMarkdown())
	if err != nil {
		t.Fatalf("parseTranscript() error = %v", err)
	}
	if got.Model != am.chosen {
		t.Errorf("Model = %q, want %q", got.Model, am.chosen)
	}
	if got.System != "be brief\nand kind" {
		t.Errorf("System = %q, want the multi-line prompt", got.System)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(got.Messages))
	}
	if !reflect.DeepEqual(got.Messages, am.messages) {
		t.Errorf("messages did not round trip:\n got: %#v\nwant: %#v", got.Messages, am.messages)
	}
}

func TestParseTranscriptRejectsGarbage(t *testing.T) {
	if _, err := parseTranscript("just some\nrandom file\n"); err == nil {
		t.Error("parseTranscript() accepted a non-transcript")
	}
}

func TestResumeOpensChatWithConversation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	tr := Transcript{
		Model:  "gpt-5.6-terra",
		System: "be brief",
		Messages: []openai.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi", Model: "gpt-5.6-terra"},
		},
	}
	var model tea.Model = New(clientFor(srv), "dark", config.Config{}, "").Resume(tr)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	am := model.(Model)
	if am.state != stateChat || am.chosen != "gpt-5.6-terra" {
		t.Fatalf("state/chosen = %v/%q, want chat on gpt-5.6-terra", am.state, am.chosen)
	}
	if len(am.messages) != 2 || am.systemPrompt != "be brief" {
		t.Errorf("conversation not restored: %d messages, prompt %q",
			len(am.messages), am.systemPrompt)
	}
	if !strings.Contains(am.viewChat(), "resumed 2 messages") {
		t.Error("chat does not show the resume notice")
	}

	// Without a client the messages are kept for after key entry.
	model = New(nil, "dark", config.Config{}, "").Resume(tr)
	am = model.(Model)
	if am.state != statePicker || len(am.messages) != 2 || am.pendingModel != "" {
		t.Errorf("keyless resume: state=%v messages=%d pendingModel=%q",
			am.state, len(am.messages), am.pendingModel)
	}
}
