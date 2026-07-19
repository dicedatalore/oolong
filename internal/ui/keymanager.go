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
	providergoogle "github.com/dicedatalore/oolong/internal/google"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/openai"
)

// keyProviders is the key manager's row order; tab/↓ and ↑ cycle through it.
var keyProviders = []keystore.Provider{keystore.OpenAI, keystore.Anthropic, keystore.Google}

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

// keyInput returns the entry field belonging to a provider's row.
func (m *Model) keyInput(provider keystore.Provider) *textinput.Model {
	switch provider {
	case keystore.Anthropic:
		return &m.anthropicKeyInput
	case keystore.Google:
		return &m.googleKeyInput
	}
	return &m.openAIKeyInput
}

func (m *Model) openKeyManager() tea.Cmd {
	m.state = stateKeyManager
	m.keyProvider = keystore.OpenAI
	m.keyErr = ""
	for _, provider := range keyProviders {
		m.keyInput(provider).SetValue("")
		m.keyInput(provider).Blur()
	}
	m.refreshKeyStatuses()
	return m.keyInput(keystore.OpenAI).Focus()
}

func (m *Model) refreshKeyStatuses() {
	m.keyStatuses = make(map[keystore.Provider]string, len(keyProviders))
	for _, provider := range keyProviders {
		m.keyStatuses[provider] = keystore.Status(provider)
	}
}

func (m *Model) selectKeyProvider(provider keystore.Provider) tea.Cmd {
	m.keyProvider = provider
	m.keyErr = ""
	for _, other := range keyProviders {
		m.keyInput(other).Blur()
	}
	return m.keyInput(provider).Focus()
}

// stepKeyProvider returns the provider delta rows away, wrapping around.
func (m Model) stepKeyProvider(delta int) keystore.Provider {
	for i, provider := range keyProviders {
		if provider == m.keyProvider {
			return keyProviders[(i+delta+len(keyProviders))%len(keyProviders)]
		}
	}
	return keyProviders[0]
}

func (m Model) closeKeyManager() (tea.Model, tea.Cmd) {
	m.keyValidating = false
	for _, provider := range keyProviders {
		m.keyInput(provider).SetValue("")
		m.keyInput(provider).Blur()
	}
	m.keyErr = ""
	m.state = statePicker
	// The old picker tick stopped scheduling when it observed the manager
	// state. Use a new tag so any stale tick is ignored and start a fresh loop.
	m.sparkleTag++
	return m, sparkleTick(m.sparkleTag)
}

func (m Model) activeKeyInput() textinput.Model {
	return *m.keyInput(m.keyProvider)
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
		case "tab", "down":
			return m, m.selectKeyProvider(m.stepKeyProvider(+1))
		case "up":
			return m, m.selectKeyProvider(m.stepKeyProvider(-1))
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
			provider := m.keyProvider
			validate := openai.ValidateKey
			switch provider {
			case keystore.Anthropic:
				validate = provideranthropic.ValidateKey
			case keystore.Google:
				validate = providergoogle.ValidateKey
			}
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				return keyCheckMsg{provider: provider, key: keyValue, err: validate(keyValue)}
			})
		}
	}

	var cmd tea.Cmd
	input := m.keyInput(m.keyProvider)
	*input, cmd = input.Update(msg)
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
	m.keyInput(msg.provider).SetValue("")
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
	switch provider {
	case keystore.Anthropic:
		return "Anthropic"
	case keystore.Google:
		return "Google"
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
	rows := make([]string, 0, len(keyProviders))
	for _, provider := range keyProviders {
		rows = append(rows, m.keyRow(provider, *m.keyInput(provider)))
	}
	body := strings.Join(rows, "\n\n")
	bottom := helpStyle.Render("tab/↑/↓: select • enter: validate/save • ctrl+d: delete • esc: back")
	if m.keyValidating {
		bottom = m.spin.View() + helpStyle.Render("validating "+providerName(m.keyProvider)+" key…")
	} else if m.keyErr != "" {
		bottom = errorStyle.Render(m.keyErr)
	}
	return pageStyle.Render(header + "\n" + body + "\n" + bottomBarStyle.Render(bottom))
}
