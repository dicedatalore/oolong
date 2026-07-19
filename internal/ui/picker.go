package ui

// The model picker screen: a filterable list of the models Oolong can talk
// to, shown beneath the animated logo. Picking one opens a fresh chat.

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
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
// the picker rows from it, grouped by provider. The catalog starts as the
// config's (or built-in) list in New and shrinks when the availability check
// drops models. Grouping copies into a fresh slice, which also keeps the
// picker's in-place effort edits away from the shared config.Builtin slice.
// In the full view each provider group sits under its own header row; the
// simple view lists the models bare.
func (m *Model) setCatalog(models []config.Model) {
	m.catalog = m.groupByProvider(models)
	m.rates = ratesFrom(m.catalog)
	items := make([]list.Item, 0, len(m.catalog)+3)
	prevProvider := ""
	for _, cm := range m.catalog {
		if p := m.resolvedProvider(cm.Provider); !m.simplePicker && p != prevProvider {
			items = append(items, headerItem{name: providerTitle(p)})
			prevProvider = p
		}
		items = append(items, newModelItem(cm))
	}
	m.picker.SetItems(items)
	m.picker.AdditionalShortHelpKeys = pickerAdditionalHelp(len(m.catalog) > 0, m.simplePicker)
	m.settleSelection()
}

// groupByProvider reorders a catalog so each provider's models sit together,
// providers in order of first appearance and models in catalog order within
// their group. The result is always a fresh slice.
func (m Model) groupByProvider(models []config.Model) []config.Model {
	var order []string
	groups := make(map[string][]config.Model)
	for _, cm := range models {
		p := m.resolvedProvider(cm.Provider)
		if _, ok := groups[p]; !ok {
			order = append(order, p)
		}
		groups[p] = append(groups[p], cm)
	}
	grouped := make([]config.Model, 0, len(models))
	for _, p := range order {
		grouped = append(grouped, groups[p]...)
	}
	return grouped
}

// resolvedProvider applies the fallback chain a model's provider field goes
// through everywhere else: the global provider, then OpenAI.
func (m Model) resolvedProvider(provider string) string {
	if provider == "" {
		provider = m.provider
	}
	if provider == "" {
		return "openai"
	}
	return provider
}

// providerTitle is the display name a provider's picker header shows.
func providerTitle(provider string) string {
	switch provider {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google"
	case "ollama":
		return "Ollama"
	}
	return provider
}

func pickerAdditionalHelp(hasModels, simple bool) func() []key.Binding {
	return func() []key.Binding {
		bindings := make([]key.Binding, 0, 3)
		if hasModels {
			bindings = append(bindings,
				key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "reasoning effort")))
		}
		viewLabel := "simple view"
		if simple {
			viewLabel = "full view"
		}
		// tab goes last: on narrow windows the help bar truncates from the
		// right, and the view toggle is the hint best afforded to lose.
		return append(bindings,
			key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "key manager")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", viewLabel)))
	}
}

// refreshBuiltinCatalog filters only the compiled-in defaults. User-defined
// catalogs are left untouched because they may describe keyless or otherwise
// custom authentication schemes.
func (m *Model) refreshBuiltinCatalog() {
	if !m.builtinCatalog {
		return
	}
	models := make([]config.Model, 0, len(config.Builtin))
	for _, model := range config.Builtin {
		provider := model.Provider
		if provider == "" {
			provider = "openai"
		}
		switch provider {
		case "ollama":
			models = append(models, model)
		case "anthropic":
			if keystore.Resolve(keystore.Anthropic) != "" {
				models = append(models, model)
			}
		case "google":
			if keystore.Resolve(keystore.Google) != "" {
				models = append(models, model)
			}
		case "openai":
			globalProvider := m.provider
			if globalProvider == "" {
				globalProvider = "openai"
			}
			if keystore.Resolve(keystore.OpenAI) != "" || (m.client != nil && globalProvider == "openai") {
				models = append(models, model)
			}
		}
	}
	m.setCatalog(models)
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

// headerItem is a provider heading above a group of models in the full view.
// It is a label, not a choice: navigation hops over it (see skipHeader) and
// its empty filter value keeps it out of filtered lists.
type headerItem struct{ name string }

func (h headerItem) FilterValue() string { return "" }

// modelItem is one row of the picker. Its three methods satisfy the list
// bubble's Item interface — Go interfaces are implemented implicitly, so
// there is no "implements" declaration to look for.
type modelItem struct {
	id     string
	desc   string
	effort string // reasoning effort shown in the title; "" for model default
}

func (m modelItem) Title() string {
	if m.effort == "" {
		return m.id
	}
	return m.id + " • effort: " + m.effort
}
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
	return modelItem{id: cm.ID, desc: desc, effort: cm.ReasoningEffort}
}

// price formats a USD rate, dropping the cents when they are zero.
func price(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// pickerDelegate renders provider header rows itself and hands model rows to
// the wrapped default delegate.
type pickerDelegate struct{ list.DefaultDelegate }

func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	h, ok := item.(headerItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	// A blank line separates the header from the group above; the top of the
	// list needs no separator. The two-space indent lines the header up with
	// the model titles beneath it.
	if index > 0 {
		io.WriteString(w, "\n")
	}
	io.WriteString(w, "  "+headerStyle.Render(h.name))
}

// newPickerDelegate builds the row renderer for the requested view: the full
// view shows a description line under each model with a blank row between,
// the simple view packs bare one-line rows.
func newPickerDelegate(simple bool) pickerDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(peach).BorderForeground(peach)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(peachDim).BorderForeground(peach)
	delegate.ShowDescription = !simple
	if simple {
		delegate.SetSpacing(0)
	}
	return pickerDelegate{delegate}
}

func newPicker(simple bool) list.Model {
	var items []list.Item
	picker := list.New(items, newPickerDelegate(simple), 0, 0)
	// Provider headers between the rows serve as the picker's titles; the
	// title bar area stays reserved for the filter input.
	picker.SetShowTitle(false)
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
	// Page-turning would swallow left/right, which adjust the selected
	// model's reasoning effort instead; up/down still walk across pages.
	picker.KeyMap.NextPage = key.NewBinding()
	picker.KeyMap.PrevPage = key.NewBinding()
	picker.KeyMap.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit"))
	picker.AdditionalShortHelpKeys = pickerAdditionalHelp(false, simple)
	return picker
}

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
		provider := cm.Provider
		if provider == "" {
			provider = m.provider
		}
		if provider == "" {
			provider = "openai"
		}
		// Other providers and per-model endpoints are outside the scope of
		// the official OpenAI model list and must survive this check.
		if provider != "openai" || cm.BaseURL != "" || msg.available[cm.ID] {
			kept = append(kept, cm)
		} else {
			dropped = append(dropped, cm.ID)
		}
	}
	switch {
	case len(kept) == 0:
		m.builtinCatalog = true
		m.refreshBuiltinCatalog()
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
		case "esc":
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
			m.picker.SetDelegate(newPickerDelegate(m.simplePicker))
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
		bottomBar = noticeStyle.Render(m.keyNotice) + "\n\n" + bottomBar
	}
	bottomBarHeight := lipgloss.Height(bottomBar)

	centered := lipgloss.Place(contentWidth, contentHeight-bottomBarHeight,
		lipgloss.Center, lipgloss.Center, view)
	return pageStyle.Render(centered + "\n" +
		lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, bottomBar))
}
