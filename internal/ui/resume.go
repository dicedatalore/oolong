package ui

// Saved transcripts are human-readable Markdown with a versioned, base64
// encoded JSON metadata block at the top. Resume reads only that block, so
// arbitrary Markdown, model ids, files, and images round-trip exactly.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dicedatalore/oolong/internal/chat"
)

const (
	metadataPrefix = "<!-- oolong-transcript:v1\n"
	metadataSuffix = "\n-->"
)

type Transcript struct {
	Model    string         `json:"model"`
	System   string         `json:"system,omitempty"`
	Messages []chat.Message `json:"messages"`
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

func encodeTranscript(t Transcript) (string, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return metadataPrefix + base64.RawStdEncoding.EncodeToString(data) + metadataSuffix, nil
}

func parseTranscript(data string) (Transcript, error) {
	if !strings.HasPrefix(data, metadataPrefix) {
		return Transcript{}, fmt.Errorf("unsupported transcript format")
	}
	end := strings.Index(data[len(metadataPrefix):], metadataSuffix)
	if end < 0 {
		return Transcript{}, fmt.Errorf("incomplete transcript metadata")
	}
	encoded := data[len(metadataPrefix) : len(metadataPrefix)+end]
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return Transcript{}, fmt.Errorf("invalid transcript metadata: %v", err)
	}
	var t Transcript
	if err := json.Unmarshal(raw, &t); err != nil {
		return Transcript{}, fmt.Errorf("invalid transcript metadata: %v", err)
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
