package ui

// Resuming a saved transcript: --resume reads a markdown file written by
// ctrl+s and reconstructs the conversation. New transcripts carry exact
// HTML-comment role markers; files saved before the markers existed fall
// back to a heading-based parse.

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/dicedatalore/oolong/internal/openai"
)

// Transcript is a conversation reconstructed from a file saved with ctrl+s.
type Transcript struct {
	Model    string // model named in the header; "" when unknown
	System   string
	Messages []openai.Message
	// DroppedAttachments notes that the saved chat had attachments, which
	// transcripts record only as labels.
	DroppedAttachments bool
}

// LoadTranscript reads a transcript saved by ctrl+s and reconstructs the
// conversation. Only ever called for a file the user explicitly named.
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

var (
	// markerRE matches the role markers transcriptMarkdown writes before
	// each block, e.g. <!--oolong:assistant gpt-5.6-terra-->.
	markerRE = regexp.MustCompile(`^<!--oolong:(user|system|assistant(?: (.+))?)-->$`)
	// attachmentRE matches the labels standing in for attachments, which
	// transcripts don't store: "_📎 2 images_", "_📄 main.go_".
	attachmentRE = regexp.MustCompile(`^_(📎 \d+ images?|📄 .+)_$`)
)

const transcriptHeader = "# Oolong chat — "

func parseTranscript(data string) (Transcript, error) {
	var t Transcript
	lines := strings.Split(data, "\n")
	if len(lines) > 0 {
		if rest, ok := strings.CutPrefix(lines[0], transcriptHeader); ok {
			t.Model = strings.TrimSpace(rest)
		}
	}

	type section struct {
		role       string
		model      string
		start, end int // content line range, marker excluded
	}
	var sections []section
	for i, line := range lines {
		match := markerRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if n := len(sections); n > 0 {
			sections[n-1].end = i
		}
		role := match[1]
		var model string
		if strings.HasPrefix(role, "assistant") {
			role, model = "assistant", match[2]
		}
		sections = append(sections, section{role: role, model: model, start: i + 1, end: len(lines)})
	}

	if len(sections) == 0 {
		t.legacyParse(lines)
	} else {
		for _, s := range sections {
			body := t.messageBody(lines[s.start:s.end])
			switch s.role {
			case "system":
				t.System = strings.TrimPrefix(body, "**System prompt:** ")
			case "user":
				t.Messages = append(t.Messages, openai.Message{Role: "user", Content: body})
			case "assistant":
				t.Messages = append(t.Messages, openai.Message{Role: "assistant", Content: body, Model: s.model})
			}
		}
	}

	if len(t.Messages) == 0 {
		return t, fmt.Errorf("no messages found — is this an Oolong transcript?")
	}
	return t, nil
}

// messageBody extracts a message's text from its section: the "## …"
// heading and any attachment labels are structure, not content.
func (t *Transcript) messageBody(lines []string) string {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.HasPrefix(lines[i], "## ") {
		i++
	}
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			i++
			continue
		}
		if attachmentRE.MatchString(trimmed) {
			t.DroppedAttachments = true
			i++
			continue
		}
		break
	}
	return strings.TrimRight(strings.Join(lines[i:], "\n"), "\n ")
}

// legacyParse handles transcripts saved before role markers existed: blocks
// are split on "## " headings outside fenced code blocks ("## You" is the
// user, anything else is a model id). A reply whose own markdown uses "## "
// headings confuses it — the markers exist so new files can't.
func (t *Transcript) legacyParse(lines []string) {
	var cur *openai.Message
	flush := func() {
		if cur == nil {
			return
		}
		cur.Content = strings.TrimSpace(cur.Content)
		t.Messages = append(t.Messages, *cur)
		cur = nil
	}
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if heading == "You" {
				cur = &openai.Message{Role: "user"}
			} else {
				cur = &openai.Message{Role: "assistant", Model: heading}
			}
			continue
		}
		if cur == nil {
			// Preamble: title, date, and (in old files) the system prompt.
			if rest, ok := strings.CutPrefix(line, "**System prompt:** "); ok {
				t.System = rest
			}
			continue
		}
		if strings.TrimSpace(cur.Content) == "" && attachmentRE.MatchString(strings.TrimSpace(line)) {
			t.DroppedAttachments = true
			continue
		}
		cur.Content += line + "\n"
	}
	flush()
}

// Resume preloads a conversation reconstructed from a saved transcript. The
// chat opens on the transcript's model once the first window size arrives;
// without a client yet (no API key), the messages are kept and the picker
// prompts the user to open the key manager.
func (m Model) Resume(t Transcript) Model {
	m.messages = t.Messages
	m.systemPrompt = t.System
	notice := fmt.Sprintf("resumed %d messages", len(t.Messages))
	if t.DroppedAttachments {
		notice += " — attachments are not saved in transcripts"
	}
	m.chatNotice = notice
	if m.client != nil && t.Model != "" {
		m.pendingModel = t.Model
	} else {
		m.keyNotice = notice
	}
	return m
}
