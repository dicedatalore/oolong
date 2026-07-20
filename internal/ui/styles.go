package ui

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// theme contains every color and style derived from configuration. It is a
// value owned by one Model, so constructing a second UI cannot inherit global
// style mutations from the first.
type theme struct {
	accent, accentDim                                      color.Color
	page, headerBar, header, inputRow, composer, bottomBar lipgloss.Style
	userLabel, botLabel, userBlock, botBlock               lipgloss.Style
	help, notice, err                                      lipgloss.Style
	logoFrom, logoTo                                       [3]int
}

func newTheme(accent, secondaryAccent string) theme {
	if accent == "" {
		accent = "#FFAF87"
	}
	v, err := strconv.ParseUint(accent[1:], 16, 32)
	if err != nil {
		v = 0xFFAF87
		accent = "#FFAF87"
	}
	r, g, b := int(v>>16&0xFF), int(v>>8&0xFF), int(v&0xFF)
	dim := func(c int) int { return int(float64(c) * 0.79) }
	primary := lipgloss.Color(accent)
	primaryDim := lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", dim(r), dim(g), dim(b)))
	if secondaryAccent == "" {
		secondaryAccent = "#7D56F4"
	}
	sv, err := strconv.ParseUint(secondaryAccent[1:], 16, 32)
	if err != nil {
		sv = 0x7D56F4
		secondaryAccent = "#7D56F4"
	}
	sr, sg, sb := int(sv>>16&0xFF), int(sv>>8&0xFF), int(sv&0xFF)
	secondary := lipgloss.Color(secondaryAccent)
	return theme{
		accent:    primary,
		accentDim: primaryDim,
		page:      lipgloss.NewStyle().Padding(1, 1),
		headerBar: lipgloss.NewStyle().Padding(0, 0, 1, 2),
		header:    lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(primary).Padding(0, 1),
		inputRow:  lipgloss.NewStyle(),
		composer:  lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, false).BorderForeground(lipgloss.Color("238")),
		bottomBar: lipgloss.NewStyle().Padding(1, 0, 0, 2),
		userLabel: lipgloss.NewStyle().Bold(true).Foreground(primary),
		botLabel:  lipgloss.NewStyle().Bold(true).Foreground(secondary),
		userBlock: lipgloss.NewStyle().Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(primary).PaddingLeft(1),
		botBlock:  lipgloss.NewStyle().Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(secondary).PaddingLeft(1),
		help:      lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		notice:    lipgloss.NewStyle().Foreground(primary),
		err:       lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87")),
		logoFrom:  [3]int{r, g, b},
		logoTo:    [3]int{sr, sg, sb},
	}
}
