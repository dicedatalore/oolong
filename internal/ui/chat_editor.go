package ui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// updateSystemPrompt handles keys while the input textarea is repurposed as
// the system prompt editor: enter saves, esc cancels, and the stashed message
// draft is restored either way.
func (m Model) updateSystemPrompt(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.exitSystemPrompt()
		return m, nil
	case "enter":
		m.systemPrompt = strings.TrimSpace(m.input.Value())
		if m.systemPrompt == "" {
			m.chatNotice = "system prompt cleared"
		} else {
			m.chatNotice = "system prompt set"
		}
		m.exitSystemPrompt()
		return m, nil
	}
	var cmd tea.Cmd
	prevHeight := m.input.Height()
	m.input, cmd = m.input.Update(key)
	if m.input.Height() != prevHeight {
		m.layoutChat()
	}
	return m, cmd
}

func (m *Model) exitSystemPrompt() {
	m.editingSystem = false
	m.input.SetValue(m.draft)
	m.draft = ""
	m.input.Placeholder = "Send a message…"
	m.help.ShowAll = false
	m.layoutChat()
}

// editorFinishedMsg reports the external editor exiting; path is the temp
// file holding the composed message.
type editorFinishedMsg struct {
	path string
	err  error
}

// openEditor hands the composer text to $VISUAL/$EDITOR via a temp file and
// returns the command that runs the editor with the terminal released. The
// result comes back to updateChat as an editorFinishedMsg.
func (m *Model) openEditor() tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		m.chatNotice = "set $EDITOR (or $VISUAL) to compose externally"
		return nil
	}
	f, err := os.CreateTemp("", "oolong-compose-*.md")
	if err != nil {
		m.errText = "editor: " + err.Error()
		return nil
	}
	path := f.Name()
	_, writeErr := f.WriteString(m.input.Value())
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(path)
		m.errText = "editor: " + writeErr.Error()
		return nil
	}
	// $EDITOR may carry arguments ("code --wait"); split on whitespace.
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

// handleEditorFinished loads the edited file back into the composer and
// cleans up the temp file. An editor that exited with an error leaves the
// composer untouched.
func (m Model) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	content, readErr := os.ReadFile(msg.path)
	_ = os.Remove(msg.path)
	if msg.err != nil {
		m.errText = "editor: " + msg.err.Error()
		return m, nil
	}
	if readErr != nil {
		m.errText = "editor: " + readErr.Error()
		return m, nil
	}
	m.input.SetValue(strings.TrimRight(string(content), "\n"))
	m.layoutChat()
	m.vp.GotoBottom()
	return m, nil
}
