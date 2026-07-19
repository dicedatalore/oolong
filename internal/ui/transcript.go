package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveTranscript writes the conversation as markdown to a timestamped file
// and returns its path. Files go to the working directory, unless a
// directory is set by OOLONG_TRANSCRIPT_DIR or, failing that, the config
// file's transcript_dir.
func (m Model) saveTranscript() (string, error) {
	name := fmt.Sprintf("oolong-chat-%s.md", time.Now().Format("2006-01-02-150405"))
	dir := os.Getenv("OOLONG_TRANSCRIPT_DIR")
	if dir == "" {
		dir = m.transcriptDir
	}
	if dir != "" {
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

// transcriptMarkdown renders the conversation as markdown. Each block is
// preceded by an HTML-comment role marker — invisible in rendered markdown —
// so --resume can reconstruct the conversation exactly instead of guessing
// at headings.
func (m Model) transcriptMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Oolong chat — %s\n\n_%s_\n\n", m.chosen, time.Now().Format("2006-01-02 15:04"))
	if m.systemPrompt != "" {
		fmt.Fprintf(&b, "<!--oolong:system-->\n**System prompt:** %s\n\n", escapeMarkers(m.systemPrompt))
	}
	for _, msg := range m.messages {
		if msg.Role == "user" {
			b.WriteString("<!--oolong:user-->\n## You\n\n")
			if n := len(msg.Images); n > 0 {
				fmt.Fprintf(&b, "_%s_\n\n", imageLabel(n))
			}
			for _, f := range msg.Files {
				fmt.Fprintf(&b, "_📄 %s_\n\n", f.Name)
			}
			if msg.Content != "" {
				fmt.Fprintf(&b, "%s\n\n", escapeMarkers(msg.Content))
			}
			continue
		}
		// Older sessions may predate per-message model tracking.
		model := msg.Model
		if model == "" {
			model = m.chosen
		}
		fmt.Fprintf(&b, "<!--oolong:assistant %s-->\n## %s\n\n%s\n\n", model, model, escapeMarkers(msg.Content))
	}
	return b.String()
}

// escapeMarkers keeps message text from being mistaken for the transcript's
// own role markers when the file is parsed back by --resume.
func escapeMarkers(s string) string {
	return strings.ReplaceAll(s, "<!--oolong:", "<!-- oolong:")
}
