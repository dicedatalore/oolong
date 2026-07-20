package ui

// The key manager stores provider credentials only in the OS keychain. It
// never reads stored secret values back into the UI; only their source/status
// is displayed.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/keystore"
	providerroute "github.com/dicedatalore/oolong/internal/provider"
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
	// The previous picker visit's tick stopped scheduling in this screen. Use
	// a new tag so a delayed tick cannot revive that old animation loop.
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
		case "tab", "down", "right":
			return m, m.selectKeyProvider(m.stepKeyProvider(+1))
		case "shift+tab", "up", "left":
			return m, m.selectKeyProvider(m.stepKeyProvider(-1))
		case "ctrl+d":
			if err := keystore.Delete(m.keyProvider); err != nil {
				m.keyErr = "couldn't delete key from OS keychain"
			} else if keystore.Status(m.keyProvider) == "environment" {
				m.keyNotice = fmt.Sprintf("%s key still supplied by environment", providerName(m.keyProvider))
			} else {
				m.keyNotice = fmt.Sprintf("%s key deleted", providerName(m.keyProvider))
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
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				return keyCheckMsg{provider: provider, key: keyValue,
					err: m.resolver.ValidateKey(providerroute.Name(provider), keyValue)}
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
	m.clients = nil
	m.refreshKeyStatuses()
	m.refreshBuiltinCatalog()
	m.keyNotice = fmt.Sprintf("%s key saved to OS keychain", providerName(msg.provider))
	m.keyErr = ""

	return m, nil
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

func (m Model) keyProviderTabs() string {
	tabs := make([]string, 0, len(keyProviders))
	for _, provider := range keyProviders {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(m.theme.accentDim)
		if provider == m.keyProvider {
			style = style.Foreground(lipgloss.Color("235")).Background(m.theme.accent).Bold(true)
		}
		tabs = append(tabs, style.Render(providerName(provider)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m Model) keyStatus(provider keystore.Provider) string {
	switch m.keyStatuses[provider] {
	case "environment":
		return m.theme.notice.Render("● Provided by " + keystore.EnvName(provider))
	case "keychain":
		return m.theme.notice.Render("● Saved on this device")
	default:
		return m.theme.help.Render("○ No key configured")
	}
}

func (m Model) keyCard() string {
	provider := m.keyProvider
	input := *m.keyInput(provider)
	// Width applies to the card's content; leave room for its padding, border,
	// and the surrounding page frame.
	width := min(56, max(20, m.width-m.theme.page.GetHorizontalFrameSize()-6))
	input.SetWidth(width - 4)

	description := "Paste a new key below. It will be validated before it is saved."
	if m.keyStatuses[provider] == "environment" {
		description = "The environment value takes priority. Saving a key here will keep it as a fallback."
	} else if m.keyStatuses[provider] == "keychain" {
		description = "Enter a new key to replace the one saved on this device."
	}
	body := m.keyStatus(provider) + "\n\n" + m.theme.help.Render(description) + "\n\n" + input.View()
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.accentDim).
		Padding(1, 2).
		Render(body)
}

func (m Model) viewKeyManager() string {
	contentWidth := m.width - m.theme.page.GetHorizontalFrameSize()
	contentHeight := m.height - m.theme.page.GetVerticalFrameSize()

	header := m.theme.headerBar.Render(m.theme.header.Render("API keys"))
	content := header + "\n" + m.keyProviderTabs() + "\n\n" + m.keyCard()
	// Keep every line aligned to the widest part of the manager when the
	// whole block is centered; otherwise Place centers each line separately.
	content = lipgloss.NewStyle().Width(lipgloss.Width(content)).Render(content)

	bottom := m.theme.help.Render("←/→ or tab: provider • enter: verify & save • ctrl+d: remove saved key • esc: back")
	if m.keyValidating {
		bottom = m.spin.View() + m.theme.help.Render("validating "+providerName(m.keyProvider)+" key…")
	} else if m.keyErr != "" {
		bottom = m.theme.err.Render(m.keyErr)
	}

	centered := lipgloss.Place(contentWidth, contentHeight-lipgloss.Height(bottom),
		lipgloss.Center, lipgloss.Center, content)
	return m.theme.page.Render(centered + "\n" +
		lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, bottom))
}
