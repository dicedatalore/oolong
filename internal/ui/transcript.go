package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// saveTranscript writes the conversation as markdown to a timestamped file in
// the working directory and returns the file name.
func (m Model) saveTranscript() (string, error) {
	name := fmt.Sprintf("oolong-chat-%s.md", time.Now().Format("2006-01-02-150405"))
	return name, os.WriteFile(name, []byte(m.transcriptMarkdown()), 0o644)
}

func (m Model) transcriptMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Oolong chat — %s\n\n_%s_\n\n", m.chosen, time.Now().Format("2006-01-02 15:04"))
	if m.systemPrompt != "" {
		fmt.Fprintf(&b, "**System prompt:** %s\n\n", m.systemPrompt)
	}
	for _, msg := range m.messages {
		if msg.Role == "user" {
			b.WriteString("## You\n\n")
			if n := len(msg.Images); n > 0 {
				fmt.Fprintf(&b, "_%s_\n\n", imageLabel(n))
			}
			if msg.Content != "" {
				fmt.Fprintf(&b, "%s\n\n", msg.Content)
			}
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", m.chosen, msg.Content)
	}
	return b.String()
}
