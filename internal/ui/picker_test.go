package ui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

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

func TestPickerEscClearsAppliedFilterBeforeQuitting(t *testing.T) {
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	am := model.(Model)
	am.state = statePicker // New(nil, …) starts on key entry; drive the picker anyway
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
