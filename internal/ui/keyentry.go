package ui

// The API key entry screen: shown on first run, or after ctrl+k on the
// picker drops the saved key. The key is validated against the API before
// being stored in the OS keychain.

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/openai"
)

// keyCheckMsg carries the result of validating an entered API key.
type keyCheckMsg struct {
	key string
	err error
}

func newKeyInput() textinput.Model {
	keyInput := textinput.New()
	keyInput.Placeholder = "sk-..."
	keyInput.EchoMode = textinput.EchoPassword
	keyInput.EchoCharacter = '•'
	keyInput.CharLimit = 0
	return keyInput
}

func (m Model) updateKeyEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.keyValidating {
			// Ignore typing while a check is in flight; esc still quits.
			if key.String() == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch key.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			k := strings.TrimSpace(m.keyInput.Value())
			if k == "" {
				m.keyErr = "API key cannot be empty"
				return m, nil
			}
			m.keyValidating = true
			m.keyErr = ""
			// Validate off the UI loop; the result arrives back in Update
			// as a keyCheckMsg, handled by handleKeyCheck.
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				return keyCheckMsg{key: k, err: openai.ValidateKey(k)}
			})
		}
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

// handleKeyCheck finishes key entry: on success the key becomes the active
// client and is saved to the keychain, and the picker takes over.
func (m Model) handleKeyCheck(msg keyCheckMsg) (tea.Model, tea.Cmd) {
	if m.state != stateKeyEntry {
		return m, nil
	}
	m.keyValidating = false
	if msg.err != nil {
		m.keyErr = msg.err.Error()
		return m, nil
	}
	m.client = openai.New(msg.key)
	if err := keystore.Set(msg.key); err != nil {
		m.keyNotice = "couldn't save to keychain; key active for this session only"
	} else {
		m.keyNotice = "key saved to OS keychain"
	}
	m.keyInput.SetValue("")
	m.keyInput.Blur()
	m.keyErr = ""
	m.state = statePicker
	m.sparkleTag++
	return m, sparkleTick(m.sparkleTag)
}

// viewKeyEntry renders the masked key input with a status line underneath.
func (m Model) viewKeyEntry() string {
	header := headerBarStyle.Render(headerStyle.Render("OpenAI API key"))
	bottomBar := helpStyle.Render("enter: save to keychain • esc: quit")
	if m.keyValidating {
		bottomBar = m.spin.View() + helpStyle.Render("validating key…")
	} else if m.keyErr != "" {
		bottomBar = errorStyle.Render(m.keyErr)
	}
	return pageStyle.Render(header + "\n" +
		inputRowStyle.Render(m.keyInput.View()) + "\n" +
		bottomBarStyle.Render(bottomBar))
}
