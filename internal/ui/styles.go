package ui

import "charm.land/lipgloss/v2"

// Colors and lipgloss styles shared across screens. Lipgloss styles are
// values: methods like Foreground return a modified copy, so deriving one
// style from another never mutates the original.
var (
	// peach is the app's primary accent and purple the secondary; the logo
	// gradient in logo.go runs between the same two colors.
	peach    = lipgloss.Color("#FFAF87")
	peachDim = lipgloss.Color("#C98B69")
	purple   = lipgloss.Color("#7D56F4")

	pageStyle = lipgloss.NewStyle().Padding(1, 1)

	// headerBarStyle/headerStyle/bottomBarStyle mirror the list bubble's
	// default TitleBar/Title/HelpStyle so both pages align.
	headerBarStyle = lipgloss.NewStyle().Padding(0, 0, 1, 2)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("235")).
			Background(peach).
			Padding(0, 1)

	// No indent: the textarea's own "┃ " prompt lines up under the
	// conversation blocks' left borders.
	inputRowStyle  = lipgloss.NewStyle()
	bottomBarStyle = lipgloss.NewStyle().Padding(1, 0, 0, 2)

	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(peach)
	botLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(purple)
	// Both sides of the conversation render in left-bordered blocks that
	// align flush left: user messages in the primary accent, model
	// replies in the secondary.
	userBlockStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(peach).
			PaddingLeft(1)
	botBlockStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(purple).
			PaddingLeft(1)
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
)
