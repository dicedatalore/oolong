package ui

import (
	"fmt"
	"image/color"
	"math/rand/v2"
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

// stripeSymbols is the pool the banner's framing rows are drawn from; a fresh
// random sequence is picked each launch. Single-cell glyphs only.
var stripeSymbols = []rune("✦✧✶✺❋·*+~")

func stripeRow(width int, theme theme) string {
	runes := make([]rune, width)
	for i := range runes {
		runes[i] = stripeSymbols[rand.IntN(len(stripeSymbols))]
	}
	return gradientRow(string(runes), theme)
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

// renderLogoHeader draws the banner shown above the model picker: rows of
// random gradient symbols framing the wordmark, with the app name and
// version on top.
func renderLogoHeader(theme theme) string {
	width := lipgloss.Width(logoRows[0])

	// The tagline sits on the left and the version on the right of the
	// same line, above the wordmark.
	tagline := lipgloss.NewStyle().Italic(true).Foreground(theme.accentDim).
		Render("simple ephemeral chat")
	ver := theme.help.Render(version.String())
	pad := max(width-lipgloss.Width(tagline)-lipgloss.Width(ver), 1)

	rows := []string{tagline + strings.Repeat(" ", pad) + ver}
	for _, r := range logoRows {
		rows = append(rows, gradientRow(r, theme))
	}
	rows = append(rows, stripeRow(width, theme))
	return strings.Join(rows, "\n")
}
