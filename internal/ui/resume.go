package ui

// Saved transcripts are readable Markdown. Resume reconstructs the conversation
// from its visible title and sections; attachments remain only as their visible
// text labels.

import (
	"fmt"
	"os"
	"strings"

	"github.com/dicedatalore/oolong/internal/chat"
)

const transcriptTitle = "# Oolong chat — "

type Transcript struct {
	Model    string
	System   string
	Messages []chat.Message
}

func LoadTranscript(path string) (Transcript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Transcript{}, err
	}
	t, err := parseTranscript(string(data))
	if err != nil {
		return Transcript{}, fmt.Errorf("%s: %v", path, err)
	}
	return t, nil
}

func parseTranscript(data string) (Transcript, error) {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(data, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], transcriptTitle) {
		return Transcript{}, fmt.Errorf("unsupported transcript format")
	}
	t := Transcript{Model: strings.TrimSpace(strings.TrimPrefix(lines[0], transcriptTitle))}
	const marker = "\n---\n\n## "
	parts := strings.Split("\n"+data, marker)
	for _, part := range parts[1:] {
		heading, content, ok := strings.Cut(part, "\n\n")
		if !ok {
			return Transcript{}, fmt.Errorf("incomplete transcript section")
		}
		content = strings.TrimRight(content, "\n")
		switch heading {
		case "System prompt":
			t.System = content
		case "You":
			t.Messages = append(t.Messages, chat.Message{Role: "user", Content: content})
		default:
			t.Messages = append(t.Messages, chat.Message{Role: "assistant", Model: heading, Content: content})
		}
	}
	if len(t.Messages) == 0 {
		return Transcript{}, fmt.Errorf("transcript contains no messages")
	}
	return t, nil
}

func (m Model) Resume(t Transcript) Model {
	m.messages = t.Messages
	m.systemPrompt = t.System
	notice := fmt.Sprintf("resumed %d messages", len(t.Messages))
	m.chatNotice = notice
	if t.Model != "" && m.clientFor(t.Model) != nil {
		m.pendingModel = t.Model
	} else {
		m.keyNotice = notice
	}
	return m
}
