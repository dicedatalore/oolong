package ui

// The model picker screen: a filterable list of the models Oolong can talk
// to, shown beneath the animated logo. Picking one opens a fresh chat.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mjcadz/oolong/internal/keystore"
)

// USD per 1M tokens. https://openai.com/api/pricing
type modelRates struct {
	input  float64
	output float64
}

var rates = map[string]modelRates{
	"gpt-5.6-luna":  {input: 1.00, output: 6.00},
	"gpt-5.6-terra": {input: 2.50, output: 15.00},
	"gpt-5.6-sol":   {input: 5.00, output: 30.00},
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
func newModelItem(id, desc string) modelItem {
	if r, ok := rates[id]; ok {
		desc += fmt.Sprintf(" • %s in / %s out per 1M tokens", price(r.input), price(r.output))
	}
	return modelItem{id: id, desc: desc}
}

// price formats a USD rate, dropping the cents when they are zero.
func price(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func newPicker() list.Model {
	items := []list.Item{
		newModelItem("gpt-5.6-luna", "For cost-sensitive workloads"),
		newModelItem("gpt-5.6-terra", "Balances intelligence and cost"),
		newModelItem("gpt-5.6-sol", "For complex professional work"),
	}
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
		view = logo + "\n\n" + view
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
