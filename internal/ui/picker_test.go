package ui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
)

// newBuiltinPicker builds a sized picker over the full built-in catalog:
// mocked keyring, both provider keys supplied via env.
func newBuiltinPicker(t *testing.T, cfg config.Config) tea.Model {
	t.Helper()
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(nil, "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	return model
}

func TestPickerHelpHasNoFullHelpToggle(t *testing.T) {
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	am := model.(Model)
	if h := am.help.View(am.picker); strings.Contains(h, "? more") {
		t.Errorf("picker help still offers full help: %q", h)
	}
}

func TestPickerHidesReasoningHelpWithoutModels(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	am := model.(Model)
	help := am.help.View(am.picker)
	if strings.Contains(help, "reasoning effort") {
		t.Errorf("empty picker shows reasoning help: %q", help)
	}
	if !strings.Contains(help, "key manager") {
		t.Errorf("empty picker hides key manager help: %q", help)
	}
}

func TestPickerNoticeUsesAccentAndBlankSeparator(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	am := model.(Model)
	am.keyNotice = "credential notice"
	view := am.viewPicker()
	if !strings.Contains(view, am.theme.notice.Render("credential notice")) {
		t.Error("picker notice is not rendered with the accent notice style")
	}
	plain := ansi.ReplaceAllString(view, "")
	lines := strings.Split(plain, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "credential notice") {
			continue
		}
		if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != "" {
			t.Error("picker notice is not followed by an empty row")
		}
		return
	}
	t.Fatal("picker notice not found")
}

func TestPickerEscClearsAppliedFilterBeforeQuitting(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	am := model.(Model)
	am.state = statePicker
	model = am

	// Type a filter and apply it with enter.
	model, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "terra" {
		model, _ = model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := model.(Model).picker.FilterState(); got != list.FilterApplied {
		t.Fatalf("FilterState = %v, want FilterApplied", got)
	}

	// First esc clears the filter instead of quitting.
	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := model.(Model).picker.FilterState(); got == list.FilterApplied {
		t.Error("esc did not clear the applied filter")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("esc with an applied filter quit the program")
		}
	}

	// Second esc, with the filter gone, quits.
	_, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc on the unfiltered picker returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("esc on the unfiltered picker did not quit")
	}
}

func TestPickerGroupsModelsUnderProviderHeaders(t *testing.T) {
	model := newBuiltinPicker(t, config.Config{})
	am := model.(Model)
	if got := pickerHeaders(am); !slices.Equal(got, []string{"OpenAI", "Anthropic"}) {
		t.Fatalf("headers = %v, want [OpenAI Anthropic]", got)
	}
	if _, ok := am.picker.Items()[0].(headerItem); !ok {
		t.Error("the first row is not a provider header")
	}
	if _, ok := am.picker.SelectedItem().(modelItem); !ok {
		t.Error("the initial selection is not a model row")
	}
	if strings.Contains(am.viewPicker(), "Pick a model") {
		t.Error("picker still shows the old title")
	}
}

func TestPickerCursorSkipsProviderHeaders(t *testing.T) {
	model := newBuiltinPicker(t, config.Config{})
	selected := func() string {
		return model.(Model).picker.SelectedItem().(modelItem).id
	}
	if got := selected(); got != "gpt-5.6-luna" {
		t.Fatalf("initial selection = %q, want the first model", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := selected(); got != "gpt-5.6-luna" {
		t.Errorf("up from the first model landed on %q, want no move", got)
	}
	for range 3 {
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := selected(); got != "claude-sonnet-5" {
		t.Errorf("down across the provider boundary landed on %q, want claude-sonnet-5", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := selected(); got != "gpt-5.6-sol" {
		t.Errorf("up across the provider boundary landed on %q, want gpt-5.6-sol", got)
	}
}

func TestPickerTabTogglesSimpleView(t *testing.T) {
	model := newBuiltinPicker(t, config.Config{})
	// Cross into the Anthropic group so the toggle has a selection to keep.
	for range 3 {
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	am := model.(Model)
	if !am.simplePicker {
		t.Fatal("tab did not enable the simple view")
	}
	if h := pickerHeaders(am); h != nil {
		t.Errorf("simple view still shows provider headers: %v", h)
	}
	if got := am.picker.SelectedItem().(modelItem).id; got != "claude-sonnet-5" {
		t.Errorf("toggling moved the selection to %q, want claude-sonnet-5", got)
	}
	if strings.Contains(am.viewPicker(), "Balanced speed") {
		t.Error("simple view still renders model descriptions")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	am = model.(Model)
	if am.simplePicker {
		t.Fatal("a second tab did not restore the full view")
	}
	if got := pickerHeaders(am); len(got) != 2 {
		t.Errorf("full view headers = %v, want both providers back", got)
	}
	if got := am.picker.SelectedItem().(modelItem).id; got != "claude-sonnet-5" {
		t.Errorf("selection after the round trip = %q, want claude-sonnet-5", got)
	}
}

func TestSimplePickerConfigStartsSimple(t *testing.T) {
	model := newBuiltinPicker(t, config.Config{SimplePicker: true})
	am := model.(Model)
	if !am.simplePicker {
		t.Fatal("simple_picker did not enable the simple view")
	}
	if h := pickerHeaders(am); h != nil {
		t.Errorf("simple view shows provider headers: %v", h)
	}
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// pickerModels returns the picker's model rows, skipping provider headers.
func pickerModels(m Model) []modelItem {
	var models []modelItem
	for _, item := range m.picker.Items() {
		if mi, ok := item.(modelItem); ok {
			models = append(models, mi)
		}
	}
	return models
}

// pickerHeaders returns the names of the picker's provider header rows.
func pickerHeaders(m Model) []string {
	var headers []string
	for _, item := range m.picker.Items() {
		if h, ok := item.(headerItem); ok {
			headers = append(headers, h.name)
		}
	}
	return headers
}

func TestPickerLogoIsCentered(t *testing.T) {
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	am := model.(Model)
	am.state = statePicker // New(nil, …) starts on key entry; render the picker anyway

	for _, line := range strings.Split(ansi.ReplaceAllString(am.viewPicker(), ""), "\n") {
		if !strings.Contains(line, "█▀▀█") { // a wordmark row
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " "))
		trail := len(line) - len(strings.TrimRight(line, " "))
		if diff := lead - trail; diff < -1 || diff > 1 {
			t.Errorf("wordmark row not centered: %d leading vs %d trailing spaces", lead, trail)
		}
		return
	}
	t.Fatal("no wordmark row found in the picker view")
}
