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

var (
	logoFrom = [3]int{0xFF, 0xAF, 0x87} // peach
	logoTo   = [3]int{0x7D, 0x56, 0xF4} // purple, matches the app accent
)

// stripeSymbols is the pool the banner's framing rows are drawn from; a fresh
// random sequence is picked each launch. Single-cell glyphs only.
var stripeSymbols = []rune("✦✧✶✺❋·*+~")

func stripeRow(width int) string {
	runes := make([]rune, width)
	for i := range runes {
		runes[i] = stripeSymbols[rand.IntN(len(stripeSymbols))]
	}
	return gradientRow(string(runes))
}

// logoColor returns the color t (0..1) of the way from logoFrom to logoTo.
func logoColor(t float64) color.Color {
	lerp := func(a, b int) int { return a + int(float64(b-a)*t+0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X",
		lerp(logoFrom[0], logoTo[0]),
		lerp(logoFrom[1], logoTo[1]),
		lerp(logoFrom[2], logoTo[2])))
}

// gradientRow styles each cell of s with a left-to-right gradient.
func gradientRow(s string) string {
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
		b.WriteString(lipgloss.NewStyle().Foreground(logoColor(t)).Render(string(r)))
	}
	return b.String()
}

// renderLogoHeader draws the banner shown above the model picker: rows of
// random gradient symbols framing the wordmark, with the app name and
// version on top.
func renderLogoHeader() string {
	width := lipgloss.Width(logoRows[0])

	// The tagline sits on the left and the version on the right of the
	// same line, above the wordmark.
	tagline := lipgloss.NewStyle().Italic(true).Foreground(peachDim).
		Render("simple ephemeral chat")
	ver := helpStyle.Render(version.String())
	pad := max(width-lipgloss.Width(tagline)-lipgloss.Width(ver), 1)

	rows := []string{tagline + strings.Repeat(" ", pad) + ver}
	for _, r := range logoRows {
		rows = append(rows, gradientRow(r))
	}
	rows = append(rows, stripeRow(width))
	return strings.Join(rows, "\n")
}
