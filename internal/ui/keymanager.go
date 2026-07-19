package ui

// The key manager stores provider credentials only in the OS keychain. It
// never reads stored secret values back into the UI; only their source/status
// is displayed.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	provideranthropic "github.com/dicedatalore/oolong/internal/anthropic"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/openai"
)

type keyCheckMsg struct {
	provider keystore.Provider
	key      string
	err      error
}

func newKeyInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.CharLimit = 0
	return input
}

func (m *Model) openKeyManager() tea.Cmd {
	m.state = stateKeyManager
	m.keyProvider = keystore.OpenAI
	m.keyErr = ""
	m.openAIKeyInput.SetValue("")
	m.anthropicKeyInput.SetValue("")
	m.refreshKeyStatuses()
	m.anthropicKeyInput.Blur()
	return m.openAIKeyInput.Focus()
}

func (m *Model) refreshKeyStatuses() {
	m.keyStatuses = map[keystore.Provider]string{
		keystore.OpenAI:    keystore.Status(keystore.OpenAI),
		keystore.Anthropic: keystore.Status(keystore.Anthropic),
	}
}

func (m *Model) selectKeyProvider(provider keystore.Provider) tea.Cmd {
	m.keyProvider = provider
	m.keyErr = ""
	if provider == keystore.Anthropic {
		m.openAIKeyInput.Blur()
		return m.anthropicKeyInput.Focus()
	}
	m.anthropicKeyInput.Blur()
	return m.openAIKeyInput.Focus()
}

func (m Model) closeKeyManager() (tea.Model, tea.Cmd) {
	m.keyValidating = false
	m.openAIKeyInput.SetValue("")
	m.anthropicKeyInput.SetValue("")
	m.openAIKeyInput.Blur()
	m.anthropicKeyInput.Blur()
	m.keyErr = ""
	m.state = statePicker
	// The old picker tick stopped scheduling when it observed the manager
	// state. Use a new tag so any stale tick is ignored and start a fresh loop.
	m.sparkleTag++
	return m, sparkleTick(m.sparkleTag)
}

func (m Model) activeKeyInput() textinput.Model {
	if m.keyProvider == keystore.Anthropic {
		return m.anthropicKeyInput
	}
	return m.openAIKeyInput
}

func (m Model) updateKeyManager(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.keyValidating {
			if key.String() == "esc" {
				return m.closeKeyManager()
			}
			return m, nil
		}
		switch key.String() {
		case "esc":
			return m.closeKeyManager()
		case "tab", "down", "up":
			if m.keyProvider == keystore.OpenAI {
				return m, m.selectKeyProvider(keystore.Anthropic)
			}
			return m, m.selectKeyProvider(keystore.OpenAI)
		case "ctrl+d":
			if err := keystore.Delete(m.keyProvider); err != nil {
				m.keyErr = "couldn't delete key from OS keychain"
			} else if keystore.Status(m.keyProvider) == "environment" {
				m.keyNotice = fmt.Sprintf("%s key still supplied by environment", providerName(m.keyProvider))
			} else {
				m.keyNotice = fmt.Sprintf("%s key deleted", providerName(m.keyProvider))
			}
			globalProvider := m.provider
			if globalProvider == "" {
				globalProvider = "openai"
			}
			if string(m.keyProvider) == globalProvider && keystore.Resolve(m.keyProvider) == "" {
				m.client = nil
			}
			m.clients = nil
			m.refreshKeyStatuses()
			m.refreshBuiltinCatalog()
			return m, nil
		case "enter":
			keyValue := strings.TrimSpace(m.activeKeyInput().Value())
			if keyValue == "" {
				m.keyErr = "API key cannot be empty"
				return m, nil
			}
			m.keyErr = ""
			m.keyValidating = true
			if m.keyProvider == keystore.OpenAI {
				return m, tea.Batch(m.spin.Tick, func() tea.Msg {
					return keyCheckMsg{provider: keystore.OpenAI, key: keyValue, err: openai.ValidateKey(keyValue)}
				})
			}
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				return keyCheckMsg{provider: keystore.Anthropic, key: keyValue, err: provideranthropic.ValidateKey(keyValue)}
			})
		}
	}

	var cmd tea.Cmd
	if m.keyProvider == keystore.Anthropic {
		m.anthropicKeyInput, cmd = m.anthropicKeyInput.Update(msg)
	} else {
		m.openAIKeyInput, cmd = m.openAIKeyInput.Update(msg)
	}
	return m, cmd
}

func (m Model) handleKeyCheck(msg keyCheckMsg) (tea.Model, tea.Cmd) {
	if m.state != stateKeyManager {
		return m, nil
	}
	m.keyValidating = false
	if msg.err != nil {
		m.keyErr = msg.err.Error()
		return m, nil
	}
	if err := keystore.Set(msg.provider, msg.key); err != nil {
		m.keyErr = "couldn't save key to OS keychain"
		return m, nil
	}
	if msg.provider == keystore.OpenAI {
		m.openAIKeyInput.SetValue("")
	} else {
		m.anthropicKeyInput.SetValue("")
	}
	globalProvider := m.provider
	if globalProvider == "" {
		globalProvider = "openai"
	}
	if string(msg.provider) == globalProvider {
		m.client = m.newClient(msg.key)
	}
	m.clients = nil
	m.refreshKeyStatuses()
	m.refreshBuiltinCatalog()
	m.keyNotice = fmt.Sprintf("%s key saved to OS keychain", providerName(msg.provider))
	m.keyErr = ""

	var cmd tea.Cmd
	if msg.provider == keystore.OpenAI && m.pendingCatalog != nil {
		if client, ok := m.client.(*openai.Client); ok {
			cmd = checkModels(client)
		}
	}
	return m, cmd
}

func providerName(provider keystore.Provider) string {
	if provider == keystore.Anthropic {
		return "Anthropic"
	}
	return "OpenAI"
}

func (m Model) keyRow(provider keystore.Provider, input textinput.Model) string {
	marker := "  "
	if m.keyProvider == provider {
		marker = "› "
	}
	label := headerStyle.Render(providerName(provider))
	status := m.keyStatuses[provider]
	if status == "" {
		status = "not set"
	}
	statusText := helpStyle.Render(" (" + status + ")")
	return marker + label + statusText + "\n" + inputRowStyle.Render(input.View())
}

func (m Model) viewKeyManager() string {
	header := headerBarStyle.Render(headerStyle.Render("API key manager"))
	body := m.keyRow(keystore.OpenAI, m.openAIKeyInput) + "\n\n" +
		m.keyRow(keystore.Anthropic, m.anthropicKeyInput)
	bottom := helpStyle.Render("tab/↑/↓: select • enter: validate/save • ctrl+d: delete • esc: back")
	if m.keyValidating {
		bottom = m.spin.View() + helpStyle.Render("validating "+providerName(m.keyProvider)+" key…")
	} else if m.keyErr != "" {
		bottom = errorStyle.Render(m.keyErr)
	}
	return pageStyle.Render(header + "\n" + body + "\n" + bottomBarStyle.Render(bottom))
}
