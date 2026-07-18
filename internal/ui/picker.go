package ui

// The model picker screen: a filterable list of the models Oolong can talk
// to, shown beneath the animated logo. Picking one opens a fresh chat.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/openai"
)

// USD per 1M tokens.
type modelRates struct {
	input  float64
	output float64
}

// setCatalog makes models the active catalog: it rebuilds the rates map and
// the picker rows from it. The catalog starts as the config's (or built-in)
// list in New and shrinks when the availability check drops models.
func (m *Model) setCatalog(models []config.Model) {
	m.catalog = models
	m.rates = ratesFrom(models)
	items := make([]list.Item, 0, len(models))
	for _, cm := range models {
		items = append(items, newModelItem(cm))
	}
	m.picker.SetItems(items)
}

// ratesFrom extracts the cost table from a catalog. Models without rates are
// left out: their chats show token counts but accrue no dollar estimate.
func ratesFrom(models []config.Model) map[string]modelRates {
	rates := make(map[string]modelRates, len(models))
	for _, cm := range models {
		if cm.InputRate > 0 || cm.OutputRate > 0 {
			rates[cm.ID] = modelRates{input: cm.InputRate, output: cm.OutputRate}
		}
	}
	return rates
}

// modelConfig returns the catalog entry for a model id; the zero Model when
// the id is not in the catalog.
func (m Model) modelConfig(id string) config.Model {
	for _, cm := range m.catalog {
		if cm.ID == id {
			return cm
		}
	}
	return config.Model{}
}

// modelItem is one row of the picker. Its three methods satisfy the list
// bubble's Item interface — Go interfaces are implemented implicitly, so
// there is no "implements" declaration to look for.
type modelItem struct {
	id   string
	desc string
}

func (m modelItem) Title() string       { return m.id }
func (m modelItem) Description() string { return m.desc }
func (m modelItem) FilterValue() string { return m.id }

// newModelItem appends the model's token costs to its description.
func newModelItem(cm config.Model) modelItem {
	desc := cm.Description
	if cm.InputRate > 0 || cm.OutputRate > 0 {
		cost := fmt.Sprintf("%s in / %s out per 1M tokens", price(cm.InputRate), price(cm.OutputRate))
		if desc != "" {
			desc += " • "
		}
		desc += cost
	}
	return modelItem{id: cm.ID, desc: desc}
}

// price formats a USD rate, dropping the cents when they are zero.
func price(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func newPicker() list.Model {
	var items []list.Item
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(peach).BorderForeground(peach)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(peachDim).BorderForeground(peach)
	picker := list.New(items, delegate, 0, 0)
	picker.Title = "Pick a model"
	picker.Styles.Title = headerStyle
	picker.Styles.ActivePaginationDot = picker.Styles.ActivePaginationDot.Foreground(peach)
	picker.SetShowStatusBar(false)
	// Help renders separately in viewPicker so the list block can be centered
	// while the command bar stays pinned to the bottom of the window.
	picker.SetShowHelp(false)
	// The picker has no full-help view, so drop "?" from its command bar.
	// A keyless binding stays disabled even when the list re-evaluates its
	// keybindings; SetEnabled(false) alone would be undone on every update.
	picker.KeyMap.ShowFullHelp = key.NewBinding()
	picker.KeyMap.CloseFullHelp = key.NewBinding()
	picker.KeyMap.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit"))
	picker.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "drop API key")),
		}
	}
	return picker
}

// modelsCheckMsg carries the result of listing the models available to the
// API key, used to vet a user-configured catalog before displaying it.
type modelsCheckMsg struct {
	available map[string]bool
	err       error
}

// checkModels asks the API which models exist. Only custom catalogs are
// checked — the built-in list is assumed good.
func checkModels(client *openai.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		available, err := client.ListModels(ctx)
		return modelsCheckMsg{available: available, err: err}
	}
}

// handleModelsCheck resolves the pending custom catalog: available models
// become the picker's catalog, unavailable ones are dropped with a notice.
// If the check itself failed the whole catalog shows — an API hiccup must
// not lock the user out of their models.
func (m Model) handleModelsCheck(msg modelsCheckMsg) (tea.Model, tea.Cmd) {
	if m.pendingCatalog == nil {
		return m, nil
	}
	pending := m.pendingCatalog
	m.pendingCatalog = nil
	if msg.err != nil {
		m.setCatalog(pending)
		m.keyNotice = "couldn't verify model availability: " + msg.err.Error()
		return m, nil
	}
	kept := make([]config.Model, 0, len(pending))
	var dropped []string
	for _, cm := range pending {
		if msg.available[cm.ID] {
			kept = append(kept, cm)
		} else {
			dropped = append(dropped, cm.ID)
		}
	}
	switch {
	case len(kept) == 0:
		m.setCatalog(config.Builtin)
		m.keyNotice = "no configured model is available — using the built-in catalog"
	case len(dropped) > 0:
		m.setCatalog(kept)
		m.keyNotice = "unavailable models hidden: " + strings.Join(dropped, ", ")
	default:
		m.setCatalog(kept)
		m.keyNotice = ""
	}
	return m, nil
}

// pickerLogo returns the banner shown above the model picker, or "" when the
// window is too narrow for the wordmark to fit without wrapping.
func (m Model) pickerLogo() string {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	if contentWidth < lipgloss.Width(logoRows[0]) {
		return ""
	}
	return m.logo
}

// sparkleMsg re-randomizes the banner's stripe row while the picker is
// showing. The tag ties a tick to the picker visit that scheduled it, so a
// stale tick from a previous visit can't start a second tick loop.
type sparkleMsg struct{ tag int }

func sparkleTick(tag int) tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return sparkleMsg{tag: tag}
	})
}

// handleSparkle redraws the logo and schedules the next tick, keeping the
// banner animated for as long as the picker is on screen.
func (m Model) handleSparkle(msg sparkleMsg) (tea.Model, tea.Cmd) {
	if m.state != statePicker || msg.tag != m.sparkleTag {
		return m, nil
	}
	m.logo = renderLogoHeader()
	return m, sparkleTick(msg.tag)
}

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.picker.FilterState() != list.Filtering {
		switch key.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "ctrl+n":
			// Discard the kept conversation and stay on the picker.
			if len(m.messages) > 0 {
				m.resetChat()
				m.keyNotice = "started a new chat"
			}
			return m, nil
		case "ctrl+k":
			keystore.Delete()
			m.client = nil
			m.keyNotice = ""
			m.keyErr = ""
			m.state = stateKeyEntry
			return m, m.keyInput.Focus()
		case "enter":
			item, ok := m.picker.SelectedItem().(modelItem)
			if !ok {
				return m, nil
			}
			return m.openChat(item.id)
		}
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

// viewPicker centers the logo and list as one block, with the command bar
// pinned to the bottom of the window.
func (m Model) viewPicker() string {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	contentHeight := m.height - pageStyle.GetVerticalFrameSize()

	// The list pads itself to its set height; trim that so the block
	// centers on its actual content.
	view := strings.TrimRight(m.picker.View(), " \n")
	if logo := m.pickerLogo(); logo != "" {
		// The logo is narrower than the list rows; center it over the
		// block so it lands centered in the window, rather than hugging
		// the list's left edge.
		w := max(lipgloss.Width(logo), lipgloss.Width(view))
		view = lipgloss.PlaceHorizontal(w, lipgloss.Center, logo) + "\n\n" + view
	}
	// Pad every line to the block's width so Place centers the block as
	// a unit; otherwise it centers each line individually and the list's
	// left edge no longer lines up.
	view = lipgloss.NewStyle().Width(lipgloss.Width(view)).Render(view)

	// The list bubble implements the help KeyMap interface itself, so the
	// help widget can render the picker's keys directly.
	bottomBar := m.help.View(m.picker)
	if m.keyNotice != "" {
		bottomBar = helpStyle.Render(m.keyNotice) + "\n" + bottomBar
	}
	bottomBarHeight := lipgloss.Height(bottomBar)

	centered := lipgloss.Place(contentWidth, contentHeight-bottomBarHeight,
		lipgloss.Center, lipgloss.Center, view)
	return pageStyle.Render(centered + "\n" +
		lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, bottomBar))
}
