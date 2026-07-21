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

// transcriptMarkdown renders the conversation as readable Markdown. Resume
// parses these same visible sections; no hidden metadata or attachment data is
// written to the file.
func (m Model) transcriptMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Oolong chat — %s\n\n_%s_\n\n", m.chosen, time.Now().Format("2006-01-02 15:04"))
	if m.systemPrompt != "" {
		writeTranscriptSection(&b, "System prompt", m.systemPrompt)
	}
	for _, msg := range m.messages {
		if msg.Role == "user" {
			var content strings.Builder
			if n := len(msg.Images); n > 0 {
				fmt.Fprintf(&content, "_%s_\n\n", imageLabel(n))
			}
			for _, f := range msg.Files {
				fmt.Fprintf(&content, "_📄 %s_\n\n", f.Name)
			}
			if msg.Content != "" {
				content.WriteString(msg.Content)
			}
			writeTranscriptSection(&b, "You", strings.TrimSpace(content.String()))
			continue
		}
		model := msg.Model
		if model == "" {
			model = m.chosen
		}
		writeTranscriptSection(&b, model, msg.Content)
	}
	return b.String()
}

func writeTranscriptSection(b *strings.Builder, heading, content string) {
	fmt.Fprintf(b, "---\n\n## %s\n\n%s\n\n", heading, content)
}
