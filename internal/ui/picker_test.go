package ui

import (
	"regexp"
	"strings"
	"testing"

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
