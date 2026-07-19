package ui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
)

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
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	am := model.(Model)
	am.keyNotice = "credential notice"
	view := am.viewPicker()
	if !strings.Contains(view, noticeStyle.Render("credential notice")) {
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

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

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
