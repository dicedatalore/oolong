package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dicedatalore/oolong/internal/version"
)

// logoRows spell OOLONG in a compact block font.
var logoRows = []string{
	"█▀▀█ █▀▀█ █    █▀▀█ █▄ █ █▀▀▀",
	"█  █ █  █ █    █  █ █ ▀█ █ ▀█",
	"▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀ ▀▀▀▀",
}

// logoColor returns the color t (0..1) of the way across the theme gradient.
func logoColor(t float64, theme theme) color.Color {
	lerp := func(a, b int) int { return a + int(float64(b-a)*t+0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X",
		lerp(theme.logoFrom[0], theme.logoTo[0]),
		lerp(theme.logoFrom[1], theme.logoTo[1]),
		lerp(theme.logoFrom[2], theme.logoTo[2])))
}

// gradientRow styles each cell of s with a left-to-right gradient.
func gradientRow(s string, theme theme) string {
	if theme.noColor {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(logoColor(t, theme)).Render(string(r)))
	}
	return b.String()
}

// renderLogoHeader draws the compact wordmark shown above the model picker.
func renderLogoHeader(theme theme) string {
	width := lipgloss.Width(logoRows[0])
	rows := make([]string, 0, len(logoRows)+1)
	for _, r := range logoRows {
		rows = append(rows, gradientRow(r, theme))
	}
	meta := theme.help.Render("simple ephemeral chat  ·  " + version.String())
	rows = append(rows, lipgloss.PlaceHorizontal(width, lipgloss.Center, meta))
	return strings.Join(rows, "\n")
}
