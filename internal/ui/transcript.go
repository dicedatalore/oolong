package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveTranscript writes the conversation as markdown to a timestamped file
// and returns its path. Files go to the working directory, or to
// OOLONG_TRANSCRIPT_DIR when set.
func (m Model) saveTranscript() (string, error) {
	name := fmt.Sprintf("oolong-chat-%s.md", time.Now().Format("2006-01-02-150405"))
	if dir := os.Getenv("OOLONG_TRANSCRIPT_DIR"); dir != "" {
		if rest, ok := strings.CutPrefix(dir, "~/"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, rest)
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		name = filepath.Join(dir, name)
	}
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
		// Older sessions may predate per-message model tracking.
		model := msg.Model
		if model == "" {
			model = m.chosen
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", model, msg.Content)
	}
	return b.String()
}
