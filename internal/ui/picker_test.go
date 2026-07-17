package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPickerHelpHasNoFullHelpToggle(t *testing.T) {
	var model tea.Model = New(nil, "dark")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	am := model.(Model)
	if h := am.help.View(am.picker); strings.Contains(h, "? more") {
		t.Errorf("picker help still offers full help: %q", h)
	}
}
