// Package ui implements the Oolong terminal user interface.
//
// The app is built on Bubble Tea, which follows the Elm architecture:
//
//   - Model is a value holding all UI state.
//   - Update receives a message (a key press, a window resize, a result
//     from the network, …) and returns the next Model plus an optional
//     tea.Cmd.
//   - View renders the current Model to the screen.
//
// A tea.Cmd is a function the runtime executes on its own goroutine; its
// return value is delivered back to Update as the next message. All I/O
// happens in commands, so Update itself never blocks.
//
// The UI is a state machine with three screens, each keeping its update and
// view code in its own file:
//
//   - picker.go   — choose a model (statePicker)
//   - chat.go     — the conversation (stateChat)
//   - keyentry.go — first-run API key prompt (stateKeyEntry)
//
// Supporting files: stream.go feeds the streamed model response into the
// chat, transcript.go saves chats to disk, keymap.go declares the chat
// keybindings, logo.go draws the animated banner, and styles.go holds the
// shared colors and styles.
package ui

import (
	"context"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/dicedatalore/oolong/internal/openai"
)

// sessionState selects which screen is active.
type sessionState int

const (
	statePicker sessionState = iota
	stateChat
	stateKeyEntry
)

// Model holds all UI state. Following Bubble Tea convention it is passed by
// value: Update works on a copy and returns that copy as the next state.
// Helpers that mutate in place (layoutChat, finishStream, …) use pointer
// receivers and operate on the copy Update is about to return.
type Model struct {
	state  sessionState
	width  int
	height int

	// model picker
	picker list.Model
	chosen string // id of the picked model, e.g. "gpt-5.6-terra"

	// chat
	input    textarea.Model // message composer
	vp       viewport.Model // scrollable conversation history
	spin     spinner.Model
	help     help.Model
	keys     chatKeyMap
	renderer *glamour.TermRenderer // markdown renderer, rebuilt on resize
	messages []openai.Message
	// msgCache[i] is messages[i] rendered at cacheWidth. Completed messages
	// render once; only the streaming message re-renders per delta.
	msgCache   []string
	cacheWidth int
	waiting    bool // a request is in flight
	errText    string

	// in-flight response stream (see stream.go)
	stream        <-chan openai.StreamEvent
	cancelStream  context.CancelFunc
	streaming     bool     // an in-progress assistant message is the last element of messages
	pendingImages [][]byte // pasted images sent with the next message

	// system prompt editing (ctrl+p repurposes the chat input)
	systemPrompt  string
	editingSystem bool   // the input textarea is editing the system prompt
	draft         string // message draft stashed while editing the system prompt
	chatNotice    string // transient status line in the chat bottom bar

	// running totals for the cost estimate in the chat header
	inputTokens  int
	outputTokens int

	// API key entry
	keyInput      textinput.Model
	keyErr        string
	keyNotice     string
	keyValidating bool

	mdStyle    string // glamour style name matching the terminal background
	client     *openai.Client
	logo       string
	sparkleTag int
	initCmd    tea.Cmd // startup command, returned by Init
}

// New builds the initial model. A nil client (no stored API key yet) starts
// the app on the key entry screen instead of the picker.
func New(client *openai.Client, mdStyle string) Model {
	m := Model{
		state:    statePicker,
		picker:   newPicker(),
		input:    newChatInput(),
		keyInput: newKeyInput(),
		spin:     newSpinner(),
		help:     help.New(),
		keys:     newChatKeyMap(),
		mdStyle:  mdStyle,
		client:   client,
		logo:     renderLogoHeader(),
	}
	m.initCmd = sparkleTick(0)
	if client == nil {
		m.state = stateKeyEntry
		m.initCmd = m.keyInput.Focus()
	}
	return m
}

func newSpinner() spinner.Model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(peach)
	return spin
}

// Init is called once by the Bubble Tea runtime and returns the first
// command to run.
func (m Model) Init() tea.Cmd {
	return m.initCmd
}

// Update is the single entry point for all messages. Messages that matter on
// every screen (resizes, quit keys, async results) are handled here; anything
// else falls through to the active screen's update function.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			// While a response is in flight, ctrl+c stops the stream and
			// returns to the input bar rather than exiting.
			if m.state == stateChat && m.waiting {
				m.finishStream()
				return m, nil
			}
			return m, tea.Quit
		}
		// Every other key goes to the active screen below.

	case sparkleMsg:
		return m.handleSparkle(msg)

	case streamEventMsg:
		return m.handleStreamEvent(msg)

	case keyCheckMsg:
		return m.handleKeyCheck(msg)

	case spinner.TickMsg:
		// Advance the spinner only while something is in flight; returning
		// no follow-up tick command ends the animation loop.
		if !m.waiting && !m.keyValidating {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	switch m.state {
	case statePicker:
		return m.updatePicker(msg)
	case stateChat:
		return m.updateChat(msg)
	case stateKeyEntry:
		return m.updateKeyEntry(msg)
	}
	return m, nil
}

// handleResize records the new window size and re-lays-out the widgets.
// Bubble Tea delivers a WindowSizeMsg at startup, so this also establishes
// the initial sizes.
func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.help.SetWidth(msg.Width - pageStyle.GetHorizontalFrameSize())
	// Reserve a line for the bottom command bar plus a gap above it,
	// and room for the logo (with a gap below) when it fits.
	pickerHeight := msg.Height - pageStyle.GetVerticalFrameSize() - 2
	if logo := m.pickerLogo(); logo != "" {
		pickerHeight -= lipgloss.Height(logo) + 1
	}
	m.picker.SetSize(
		msg.Width-pageStyle.GetHorizontalFrameSize(),
		pickerHeight,
	)
	// SetSize stretches the filter input to the list's full width, which
	// makes the centered block span the whole window while filtering.
	// Cap it so the filter row stays about as wide as the list items.
	m.picker.FilterInput.SetWidth(20)
	if m.state == stateChat {
		m.layoutChat()
	}
	return m, nil
}

// View renders the active screen. It is a pure function of the model: no
// state changes, just formatting.
func (m Model) View() tea.View {
	v := tea.NewView("loading…")
	v.AltScreen = true
	// Report mouse events so the wheel scrolls the chat viewport; without
	// this, terminals fake wheel input as arrow keys, which now belong to
	// the textarea. Text selection needs shift-drag, as usual in TUIs.
	v.MouseMode = tea.MouseModeCellMotion
	if m.width == 0 {
		// The first WindowSizeMsg hasn't arrived; sizes are still unknown.
		return v
	}

	switch m.state {
	case statePicker:
		v.SetContent(m.viewPicker())
	case stateKeyEntry:
		v.SetContent(m.viewKeyEntry())
	case stateChat:
		v.SetContent(m.viewChat())
	}
	return v
}
