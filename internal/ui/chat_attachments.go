package ui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/dicedatalore/oolong/internal/chat"
)

// newFilePicker builds the attach-file picker, starting in the working
// directory.
func newFilePicker(height int, theme theme) filepicker.Model {
	fp := filepicker.New()
	if dir, err := os.Getwd(); err == nil {
		fp.CurrentDirectory = dir
	}
	fp.AutoHeight = false
	fp.SetHeight(height)
	// esc must cancel the picker (handled in updateFilePicker), not walk up
	// a directory.
	fp.KeyMap.Back = key.NewBinding(key.WithKeys("h", "backspace", "left"), key.WithHelp("h", "back"))
	fp.Styles.Cursor = fp.Styles.Cursor.Foreground(theme.accent)
	fp.Styles.Selected = fp.Styles.Selected.Foreground(theme.accent).Bold(true)
	return fp
}

// updateFilePicker routes messages while the attach-file picker overlays the
// conversation: esc cancels, choosing a file loads it as an attachment.
func (m Model) updateFilePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		m.pickingFile = false
		m.layoutChat()
		return m, nil
	}
	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.Update(msg)
	if ok, path := m.filePicker.DidSelectFile(msg); ok {
		m.pickingFile = false
		m.attachPath(path)
		m.layoutChat()
	}
	return m, cmd
}

// attachPath loads a file from disk as an attachment: images join the
// pending images, anything that reads as text becomes a pending file block.
func (m *Model) attachPath(path string) {
	// Past ~a megabyte a text file wouldn't fit a context window anyway,
	// and images meet API limits long before this.
	const maxAttachment = 20 << 20
	if info, err := os.Stat(path); err == nil && info.Size() > maxAttachment {
		m.chatNotice = filepath.Base(path) + " is too large to attach (20MB max)"
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.errText = "attach: " + err.Error()
		return
	}
	name := filepath.Base(path)
	switch mime := http.DetectContentType(data); mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		m.pendingImages = append(m.pendingImages, data)
		m.chatNotice = "attached " + name
	default:
		if !utf8.Valid(data) {
			m.chatNotice = name + " is neither an image nor text"
			return
		}
		m.pendingFiles = append(m.pendingFiles, chat.File{Name: name, Text: string(data)})
		m.chatNotice = "attached " + name
	}
}

func (m Model) attachmentLabel() string {
	return strings.Join(m.attachmentItems(), " • ")
}

func imageLabel(n int) string {
	if n == 1 {
		return "📎 1 image"
	}
	return fmt.Sprintf("📎 %d images", n)
}
