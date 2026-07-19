package ui

import (
	"net/http"
	"net/http/httptest"
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
		// A reply full of the things the legacy parser chokes on: its own
		// "## " headings and a fenced block containing one.
		{Role: "assistant", Model: "gpt-5.6-terra", Content: "## Sure\n\n```md\n## fenced heading\n```\n\ndone"},
		{Role: "user", Content: "thanks", Images: [][]byte{{1}}},
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
	for i, want := range am.messages {
		if got.Messages[i].Role != want.Role || got.Messages[i].Content != want.Content {
			t.Errorf("message %d = %+v, want role %q content %q",
				i, got.Messages[i], want.Role, want.Content)
		}
	}
	if got.Messages[1].Model != "gpt-5.6-terra" {
		t.Errorf("reply model = %q, want gpt-5.6-terra", got.Messages[1].Model)
	}
	if !got.DroppedAttachments {
		t.Error("DroppedAttachments not set for a chat with an image")
	}
}

func TestParseLegacyTranscript(t *testing.T) {
	legacy := `# Oolong chat — gpt-5.6-sol

_2026-01-02 15:04_

**System prompt:** be brief

## You

hello there

## gpt-5.6-sol

hi!

` + "```go\n## not a heading\n```" + `

## You

_📎 1 image_

what is this
`
	got, err := parseTranscript(legacy)
	if err != nil {
		t.Fatalf("parseTranscript() error = %v", err)
	}
	if got.Model != "gpt-5.6-sol" || got.System != "be brief" {
		t.Errorf("Model/System = %q/%q, want gpt-5.6-sol/be brief", got.Model, got.System)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(got.Messages))
	}
	if !strings.Contains(got.Messages[1].Content, "## not a heading") {
		t.Error("fenced heading split the reply")
	}
	if got.Messages[2].Content != "what is this" || !got.DroppedAttachments {
		t.Errorf("attachment label mishandled: %+v dropped=%v",
			got.Messages[2], got.DroppedAttachments)
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
	if am.state != stateKeyEntry || len(am.messages) != 2 || am.pendingModel != "" {
		t.Errorf("keyless resume: state=%v messages=%d pendingModel=%q",
			am.state, len(am.messages), am.pendingModel)
	}
}
