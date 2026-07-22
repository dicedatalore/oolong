package ui

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/config"
)

// settleSelection moves the selection off a provider header, which can end up
// selected after the rows are rebuilt.
func (m *Model) settleSelection() {
	if _, ok := m.picker.SelectedItem().(headerItem); !ok {
		return
	}
	if i := m.picker.Index() + 1; i < len(m.picker.VisibleItems()) {
		m.picker.Select(i)
	}
}

// skipHeader hops the cursor over a provider header after the list moves,
// continuing in the direction of travel; from the top-of-list header the only
// way is down. prev is the selected index before the move.
func (m *Model) skipHeader(prev int) {
	// Two hops cover the worst case (the top of the list); headers are never
	// adjacent, so a hop always lands on a model row.
	for range 2 {
		if _, ok := m.picker.SelectedItem().(headerItem); !ok {
			return
		}
		if i := m.picker.Index(); i == 0 || i > prev {
			m.picker.CursorDown()
		} else {
			m.picker.CursorUp()
		}
	}
}

// selectModel puts the cursor on the given model's row, if it is visible.
func (m *Model) selectModel(id string) {
	for i, item := range m.picker.VisibleItems() {
		if mi, ok := item.(modelItem); ok && mi.id == id {
			m.picker.Select(i)
			return
		}
	}
}

// stepEffort moves one step along the effort ladder — model default at the
// bottom, then config.Efforts in order — clamping at both ends.
func stepEffort(cur string, delta int) string {
	ladder := append([]string{""}, config.Efforts...)
	i := slices.Index(ladder, cur)
	if i < 0 {
		// A config-supplied level Oolong doesn't know; leave it alone.
		return cur
	}
	i = min(max(i+delta, 0), len(ladder)-1)
	return ladder[i]
}

// adjustEffort steps the selected model's reasoning effort and refreshes its
// row. The catalog entry itself changes, so the setting rides along into the
// chat (and back, if the user returns to the picker).
func (m *Model) adjustEffort(delta int) tea.Cmd {
	item, ok := m.picker.SelectedItem().(modelItem)
	if !ok {
		return nil
	}
	for i := range m.catalog {
		if m.catalog[i].ID != item.id {
			continue
		}
		m.catalog[i].ReasoningEffort = stepEffort(m.catalog[i].ReasoningEffort, delta)
		return m.picker.SetItem(m.picker.GlobalIndex(), newModelItem(m.catalog[i]))
	}
	return nil
}

// pickerLogo returns the banner shown above the model picker, or "" when the
// window is too narrow for the wordmark to fit without wrapping.
func (m Model) pickerLogo() string {
	contentWidth := m.width - m.pageStyle().GetHorizontalFrameSize()
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
	m.logo = renderLogoHeader(m.theme)
	return m, m.sparkleCmd()
}

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.picker.FilterState() != list.Filtering {
		switch key.String() {
		case "esc":
			if m.retryModel {
				m.retryModel = false
				m.state = stateChat
				m.keyNotice = ""
				m.chatNotice = "model retry cancelled"
				m.layoutChat()
				return m, m.input.Focus()
			}
			// With a filter applied, esc backs out of the filter first;
			// only an unfiltered picker quits. (While the filter is being
			// typed, esc never reaches here — the list cancels it.)
			if m.picker.FilterState() == list.FilterApplied {
				m.picker.ResetFilter()
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+n":
			// Discard the kept conversation and stay on the picker.
			if len(m.messages) > 0 {
				m.resetChat()
				m.keyNotice = "started a new chat"
			}
			return m, nil
		case "ctrl+k":
			return m, m.openKeyManager()
		case "tab":
			// Toggle between the full view (descriptions, rates, provider
			// headers) and the simple one-line view, keeping the selection.
			var selected string
			if item, ok := m.picker.SelectedItem().(modelItem); ok {
				selected = item.id
			}
			m.picker.ResetFilter()
			m.simplePicker = !m.simplePicker
			m.picker.SetDelegate(newPickerDelegate(m.simplePicker, m.theme))
			m.setCatalog(m.catalog)
			m.selectModel(selected)
			return m, nil
		case "left":
			return m, m.adjustEffort(-1)
		case "right":
			return m, m.adjustEffort(+1)
		case "enter":
			item, ok := m.picker.SelectedItem().(modelItem)
			if !ok {
				return m, nil
			}
			if m.clientFor(item.id) == nil {
				m.keyNotice = "no API key for this provider — ctrl+k opens the key manager"
				return m, nil
			}
			if m.retryModel {
				return m.retryLastWithModel(item.id)
			}
			return m.openChat(item.id)
		}
	}
	var cmd tea.Cmd
	prev := m.picker.Index()
	m.picker, cmd = m.picker.Update(msg)
	m.skipHeader(prev)
	return m, cmd
}

// viewPicker centers the logo and list as one block, with the command bar
// pinned to the bottom of the window.
func (m Model) viewPicker() string {
	page := m.pageStyle()
	contentWidth := max(1, m.width-page.GetHorizontalFrameSize())
	contentHeight := max(1, m.height-page.GetVerticalFrameSize())

	// The list pads itself to its set height; trim that so the block
	// centers on its actual content.
	view := strings.TrimRight(m.picker.View(), " \n")
	if len(m.catalog) == 0 {
		view = m.firstRunView(contentWidth)
	}
	if logo := m.pickerLogo(); logo != "" {
		if m.simplePicker {
			view = centerPickerBlock(view, lipgloss.Width(logo))
		}
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
	if contentWidth < 40 || contentHeight < 12 {
		bottomBar = m.theme.help.Render("ctrl+k keys • esc quit")
	}
	if m.retryModel {
		bottomBar = m.theme.notice.Render("choose a model to retry the last response • esc cancels") + "\n\n" + bottomBar
	}
	compactSetupNotice := (contentWidth < 40 || contentHeight < 12) && len(m.catalog) == 0 &&
		strings.HasPrefix(m.keyNotice, "no API keys configured")
	if m.keyNotice != "" && !compactSetupNotice {
		bottomBar = m.theme.notice.Render(m.keyNotice) + "\n\n" + bottomBar
	}
	bottomBar = centeredBar(contentWidth, bottomBar)
	bottomBarHeight := lipgloss.Height(bottomBar)

	centered := lipgloss.Place(contentWidth, max(1, contentHeight-bottomBarHeight),
		lipgloss.Center, lipgloss.Center, view)
	return page.Render(centered + "\n" + bottomBar)
}
