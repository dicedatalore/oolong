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
//   - keymanager.go — provider credential management (stateKeyManager)
//
// Supporting files: stream.go feeds the streamed model response into the
// chat, transcript.go saves chats to disk, keymap.go declares the chat
// keybindings, logo.go draws the animated banner, and styles.go holds the
// shared colors and styles.
package ui

import (
	"context"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/dicedatalore/oolong/internal/chat"
	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	providerroute "github.com/dicedatalore/oolong/internal/provider"
)

// sessionState selects which screen is active.
type sessionState int

const (
	statePicker sessionState = iota
	stateChat
	stateKeyManager
)

// Model holds all UI state. Following Bubble Tea convention it is passed by
// value: Update works on a copy and returns that copy as the next state.
// Helpers that mutate in place (layoutChat, finishStream, …) use pointer
// receivers and operate on the copy Update is about to return.
type Model struct {
	state  sessionState
	width  int
	height int
	theme  theme

	// model catalog (from the config file, or the built-in list)
	catalog        []config.Model
	builtinCatalog bool // active catalog is the compiled-in defaults
	rates          map[string]modelRates
	pendingModel   string // default_model to open once the first resize arrives
	transcriptDir  string // transcript_dir from config; the env var wins

	// Clients are cached by resolved provider and endpoint.
	clients map[string]chat.Client

	// model picker
	picker       list.Model
	simplePicker bool   // compact one-line rows; tab toggles, simple_picker seeds
	chosen       string // id of the picked model, e.g. "gpt-5.6-terra"
	retryModel   bool   // picker is choosing a model to retry the last request

	// chat
	input    textarea.Model // message composer
	vp       viewport.Model // scrollable conversation history
	spin     spinner.Model
	help     help.Model
	keys     chatKeyMap
	renderer *glamour.TermRenderer // markdown renderer, rebuilt on resize
	messages []chat.Message
	// msgCache[i] is messages[i] rendered at cacheWidth. Completed messages
	// render once; only the streaming message re-renders per delta.
	msgCache        []string
	cacheWidth      int
	waiting         bool // a request is in flight
	errText         string
	errorInfo       *chatErrorInfo
	showErrorDetail bool
	newOutputBelow  bool // streamed output arrived while the viewport was scrolled up
	contextWarning  bool
	contextPending  chat.Message
	contextResend   bool // warning applies to retrying the latest user turn

	// in-flight response stream (see stream.go)
	stream        <-chan chat.StreamEvent
	cancelStream  context.CancelFunc
	streaming     bool        // an in-progress assistant message is the last element of messages
	pendingImages [][]byte    // pasted/attached images sent with the next message
	pendingFiles  []chat.File // text files attached from disk, sent with the next message

	// attach-file picker (ctrl+f overlays the conversation)
	filePicker  filepicker.Model
	pickingFile bool

	// ↑/↓ history recall: the composer steps through previously sent
	// messages while it holds an unedited recall (see recallActive).
	recallIdx         int         // index into messages of the recalled message; -1 when none
	recallText        string      // the text recallIdx was recalled as, to detect edits
	recallSavedImages [][]byte    // pendingImages stashed when recall started
	recallSavedFiles  []chat.File // pendingFiles stashed when recall started

	// ctrl+u edits the latest user turn in place. The existing conversation is
	// left untouched until the edited prompt is sent, so esc can cancel safely.
	editIndex       int
	editSavedText   string
	editSavedImages [][]byte
	editSavedFiles  []chat.File

	// system prompt editing (ctrl+p repurposes the chat input)
	systemPrompt  string
	editingSystem bool   // the input textarea is editing the system prompt
	draft         string // message draft stashed while editing the system prompt
	chatNotice    string // transient status line in the chat bottom bar

	// running totals for the cost estimate in the chat header; cost is
	// accumulated per request at the rates of the model that served it,
	// so it stays accurate when the model changes mid-chat. While a
	// request is in flight the header adds a local estimate (the server
	// only reports usage once a response completes), which is settled
	// into the totals if the stream is stopped early.
	inputTokens    int
	outputTokens   int
	costUSD        float64
	estInputTokens int  // estimated input tokens of the in-flight request
	usageEstimated bool // session totals contain locally estimated usage

	// API key manager. Inputs contain only newly typed values and are cleared
	// immediately after a keychain save; stored secrets are never loaded here.
	openAIKeyInput    textinput.Model
	anthropicKeyInput textinput.Model
	googleKeyInput    textinput.Model
	keyProvider       keystore.Provider
	keyStatuses       map[keystore.Provider]string
	keyErr            string
	keyNotice         string
	keyValidating     bool
	spinnerColorStep  int // position in the primary ↔ secondary fade cycle

	mdStyle    string // glamour style name matching the terminal background
	resolver   *providerroute.Resolver
	logo       string
	sparkleTag int
	initCmd    tea.Cmd // startup command, returned by Init
}

// New builds the initial model. The picker remains available without keys and
// points to the key manager; cfgErr is surfaced without blocking launch.
func New(client chat.Client, mdStyle string, cfg config.Config, cfgErr string) Model {
	resolver := providerroute.NewResolver(cfg)
	if client != nil {
		// New's injected client stands in for the configured provider's global
		// route. Per-model endpoints must still construct their own client.
		injectedProviderName := providerroute.Name(cfg.Provider)
		if injectedProviderName == "" {
			injectedProviderName = resolver.RouteFor(resolver.FirstAvailableModel()).Provider
		}
		injectedRoute := providerroute.Route{Provider: injectedProviderName, BaseURL: cfg.BaseURL}
		injectedProvider, _ := providerroute.KeyProvider(injectedProviderName)
		resolveKey := resolver.ResolveKey
		resolver.ResolveKey = func(provider keystore.Provider) string {
			if provider == injectedProvider {
				return "injected"
			}
			return resolveKey(provider)
		}
		build := resolver.BuildClient
		resolver.BuildClient = func(route providerroute.Route, key string) chat.Client {
			if route.Provider == injectedRoute.Provider && route.BaseURL == injectedRoute.BaseURL {
				return client
			}
			return build(route, key)
		}
	}
	return newModel(resolver, mdStyle, cfg, cfgErr)
}

// NewWithResolver builds the production UI around the shared route resolver.
func NewWithResolver(resolver *providerroute.Resolver, mdStyle string, cfg config.Config, cfgErr string) Model {
	return newModel(resolver, mdStyle, cfg, cfgErr)
}

func newModel(resolver *providerroute.Resolver, mdStyle string, cfg config.Config, cfgErr string) Model {
	theme := newTheme(cfg.Accent, cfg.SecondaryAccent)
	m := Model{
		theme:             theme,
		state:             statePicker,
		picker:            newPicker(cfg.SimplePicker, theme),
		simplePicker:      cfg.SimplePicker,
		input:             newChatInput(theme),
		openAIKeyInput:    newKeyInput("sk-..."),
		anthropicKeyInput: newKeyInput("sk-ant-..."),
		googleKeyInput:    newKeyInput("AIza..."),
		keyProvider:       keystore.OpenAI,
		spin:              newSpinner(theme),
		help:              help.New(),
		keys:              newChatKeyMap(),
		mdStyle:           mdStyle,
		resolver:          resolver,
		logo:              renderLogoHeader(theme),
		transcriptDir:     cfg.TranscriptDir,
		recallIdx:         -1,
		editIndex:         -1,
	}
	m.initCmd = sparkleTick(0)
	if cfg.CustomCatalog() {
		m.setCatalog(cfg.Catalog())
	} else {
		m.builtinCatalog = true
		m.refreshBuiltinCatalog()
	}
	if cfg.DefaultModel != "" && m.clientFor(cfg.DefaultModel) != nil {
		// Skip the picker, but only once the first WindowSizeMsg supplies
		// real dimensions — opening the chat now would lay out at zero size.
		m.pendingModel = cfg.DefaultModel
	}
	if cfgErr != "" {
		// keyNotice shows on the picker; chatNotice covers the
		// default_model path that skips straight past it.
		m.keyNotice = cfgErr
		m.chatNotice = cfgErr
	}
	if !resolver.AnyAvailable() && m.keyNotice == "" {
		m.keyNotice = "no API keys configured — ctrl+k opens the key manager"
	}
	return m
}

// clientFor returns the cached client for a fully resolved model route.
func (m *Model) clientFor(id string) chat.Client {
	route := m.resolver.RouteFor(id)
	cacheKey := string(route.Provider) + "\x00" + route.BaseURL
	if c, ok := m.clients[cacheKey]; ok {
		return c
	}
	if m.clients == nil {
		m.clients = make(map[string]chat.Client)
	}
	c := m.resolver.ClientFor(id)
	if c == nil {
		return nil
	}
	m.clients[cacheKey] = c
	return c
}

func newSpinner(theme theme) spinner.Model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(logoColor(0, theme))
	return spin
}

// spinnerFadePosition moves from primary to secondary and back over a
// 32-frame cycle, avoiding a hard color jump when the animation loops.
func spinnerFadePosition(step int) float64 {
	const halfCycle = 16
	step %= halfCycle * 2
	if step < 0 {
		step += halfCycle * 2
	}
	if step > halfCycle {
		step = halfCycle*2 - step
	}
	return float64(step) / halfCycle
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
			// Quit from anywhere; esc stops an in-flight response.
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
		m.spinnerColorStep++
		m.spin.Style = m.spin.Style.Foreground(logoColor(spinnerFadePosition(m.spinnerColorStep), m.theme))
		return m, cmd
	}

	switch m.state {
	case statePicker:
		return m.updatePicker(msg)
	case stateChat:
		return m.updateChat(msg)
	case stateKeyManager:
		return m.updateKeyManager(msg)
	}
	return m, nil
}

// handleResize records the new window size and re-lays-out the widgets.
// Bubble Tea delivers a WindowSizeMsg at startup, so this also establishes
// the initial sizes.
func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.help.SetWidth(msg.Width - m.theme.page.GetHorizontalFrameSize())
	// Reserve a line for the bottom command bar plus a gap above it,
	// and room for the logo (with a gap below) when it fits.
	pickerHeight := msg.Height - m.theme.page.GetVerticalFrameSize() - 2
	if logo := m.pickerLogo(); logo != "" {
		pickerHeight -= lipgloss.Height(logo) + 1
	}
	m.picker.SetSize(
		msg.Width-m.theme.page.GetHorizontalFrameSize(),
		pickerHeight,
	)
	// SetSize stretches the filter input to the list's full width, which
	// makes the centered block span the whole window while filtering.
	// Cap it so the filter row stays about as wide as the list items.
	m.picker.FilterInput.SetWidth(20)
	if m.state == stateChat {
		m.layoutChat()
	}
	if m.pendingModel != "" && m.state == statePicker {
		// default_model skips the picker: open its chat now that the
		// window dimensions are known.
		id := m.pendingModel
		m.pendingModel = ""
		return m.openChat(id)
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
	case stateKeyManager:
		v.SetContent(m.viewKeyManager())
	case stateChat:
		v.SetContent(m.viewChat())
	}
	return v
}
