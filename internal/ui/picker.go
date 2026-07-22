package ui

// The model picker screen: a filterable list of the models Oolong can talk
// to, shown beneath the animated logo. Picking one opens a fresh chat.

import (
	"fmt"
	"io"
	"math"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/config"
)

// USD per 1M tokens.
type modelRates struct {
	input  float64
	output float64
}

// setCatalog makes models the active catalog: it rebuilds the rates map and
// the picker rows from it, grouped by provider. Grouping copies into a fresh
// slice, which also keeps the
// picker's in-place effort edits away from the shared config.DefaultModels slice.
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

// resolvedProvider applies the display fallback for an omitted provider.
func (m Model) resolvedProvider(provider string) string {
	if provider == "" {
		provider = string(m.resolver.RouteFor("").Provider)
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
	models := make([]config.Model, 0, len(config.DefaultModels))
	for _, model := range config.DefaultModels {
		if m.resolver.Available(m.resolver.RouteFor(model.ID)) {
			models = append(models, model)
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
type pickerDelegate struct {
	list.DefaultDelegate
	theme theme
}

func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	h, ok := item.(headerItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	label := lipgloss.NewStyle().
		Foreground(d.theme.accentDim).
		Bold(true).
		Render(strings.ToUpper(h.name))
	io.WriteString(w, "  "+label)
}

// newPickerDelegate builds the row renderer for the requested view: the full
// view shows a description line under each model with a blank row between,
// the simple view packs bare one-line rows.
func newPickerDelegate(simple bool, theme theme) pickerDelegate {
	delegate := list.NewDefaultDelegate()
	if theme.noColor {
		delegate.Styles.NormalTitle = lipgloss.NewStyle()
		delegate.Styles.NormalDesc = lipgloss.NewStyle().Faint(true)
		delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Bold(true)
		delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Bold(true)
	} else {
		delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color("252"))
		delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color("241"))
		delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
			Foreground(theme.accent).BorderForeground(theme.accent).Bold(true)
		delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
			Foreground(lipgloss.Color("245")).BorderForeground(theme.accent)
	}
	delegate.ShowDescription = !simple
	delegate.SetSpacing(0)
	return pickerDelegate{DefaultDelegate: delegate, theme: theme}
}

func newPicker(simple bool, theme theme) list.Model {
	var items []list.Item
	picker := list.New(items, newPickerDelegate(simple, theme), 0, 0)
	// Provider headers between the rows serve as the picker's titles; the
	// title bar area stays reserved for the filter input.
	picker.SetShowTitle(false)
	picker.FilterInput.Prompt = "Search  "
	picker.Styles.TitleBar = picker.Styles.TitleBar.PaddingLeft(2).PaddingBottom(1)
	picker.Styles.Filter.Focused.Prompt = picker.Styles.Filter.Focused.Prompt.Foreground(theme.accent)
	picker.Styles.Filter.Focused.Text = picker.Styles.Filter.Focused.Text.Foreground(lipgloss.Color("252"))
	picker.Styles.ActivePaginationDot = picker.Styles.ActivePaginationDot.Foreground(theme.accent)
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

// firstRunView replaces the list's generic empty state with a short setup path.
// It only appears when there are no selectable models, so configured users keep
// the compact picker they expect.
func (m Model) firstRunView(contentWidth int) string {
	width := max(1, min(58, contentWidth-4))
	title := m.theme.userLabel.Render("Welcome to Oolong")
	if contentWidth < 40 || m.height < 14 {
		return lipgloss.NewStyle().Width(width).Render(
			title + "\n\n" + m.theme.notice.Render("ctrl+k") + " add an API key\n" +
				m.theme.help.Render("Run `oolong config init` for local models."))
	}
	intro := "Choose how you want to connect:"
	actions := strings.Join([]string{
		m.theme.notice.Render("ctrl+k") + "  Add an OpenAI, Anthropic, or Google API key",
		m.theme.notice.Render("config") + "  Run `oolong config init` for Ollama or another endpoint",
		m.theme.notice.Render("doctor") + "  Run `oolong doctor` to check this setup",
	}, "\n")
	privacy := m.theme.help.Render("Keys stay in your OS keychain; environment variables win.")
	return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + intro + "\n\n" + actions + "\n\n" + privacy)
}

// centerPickerBlock centers a multiline list as one aligned block. Using
// PlaceHorizontal directly would center each model name independently and
// leave their starting columns ragged.
func centerPickerBlock(view string, width int) string {
	viewWidth := lipgloss.Width(view)
	if viewWidth >= width {
		return view
	}
	return lipgloss.NewStyle().
		Width(viewWidth).
		MarginLeft((width - viewWidth) / 2).
		Render(view)
}
