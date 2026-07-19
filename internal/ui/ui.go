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

	provideranthropic "github.com/dicedatalore/oolong/internal/anthropic"
	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/ollama"
	"github.com/dicedatalore/oolong/internal/openai"
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

	// model catalog (from the config file, or the built-in list)
	catalog        []config.Model
	builtinCatalog bool // active catalog is the compiled-in defaults
	rates          map[string]modelRates
	pendingCatalog []config.Model // custom catalog awaiting the availability check
	pendingModel   string         // default_model to open once the first resize arrives
	transcriptDir  string         // transcript_dir from config; the env var wins
	baseURL        string         // global base_url from config; blank when the env var overrides
	provider       string         // global endpoint protocol; blank means OpenAI

	// clients for models with their own base_url, keyed by endpoint and
	// built on first use; m.client serves the global endpoint.
	clients map[string]openai.ChatClient

	// model picker
	picker       list.Model
	simplePicker bool   // compact one-line rows; tab toggles, simple_picker seeds
	chosen       string // id of the picked model, e.g. "gpt-5.6-terra"

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
	streaming     bool          // an in-progress assistant message is the last element of messages
	pendingImages [][]byte      // pasted/attached images sent with the next message
	pendingFiles  []openai.File // text files attached from disk, sent with the next message

	// attach-file picker (ctrl+f overlays the conversation)
	filePicker  filepicker.Model
	pickingFile bool

	// ↑/↓ history recall: the composer steps through previously sent
	// messages while it holds an unedited recall (see recallActive).
	recallIdx         int           // index into messages of the recalled message; -1 when none
	recallText        string        // the text recallIdx was recalled as, to detect edits
	recallSavedImages [][]byte      // pendingImages stashed when recall started
	recallSavedFiles  []openai.File // pendingFiles stashed when recall started

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
	estInputTokens int // estimated input tokens of the in-flight request

	// API key manager. Inputs contain only newly typed values and are cleared
	// immediately after a keychain save; stored secrets are never loaded here.
	openAIKeyInput    textinput.Model
	anthropicKeyInput textinput.Model
	keyProvider       keystore.Provider
	keyStatuses       map[keystore.Provider]string
	keyErr            string
	keyNotice         string
	keyValidating     bool

	mdStyle    string // glamour style name matching the terminal background
	client     openai.ChatClient
	logo       string
	sparkleTag int
	initCmd    tea.Cmd // startup command, returned by Init
}

// New builds the initial model. The picker remains available without keys and
// points to the key manager; cfgErr is surfaced without blocking launch.
func New(client openai.ChatClient, mdStyle string, cfg config.Config, cfgErr string) Model {
	if cfg.Accent != "" {
		// Before the widgets below copy the accent into their own styles.
		applyAccent(cfg.Accent)
	}
	m := Model{
		state:             statePicker,
		picker:            newPicker(cfg.SimplePicker),
		simplePicker:      cfg.SimplePicker,
		input:             newChatInput(),
		openAIKeyInput:    newKeyInput("sk-..."),
		anthropicKeyInput: newKeyInput("sk-ant-..."),
		keyProvider:       keystore.OpenAI,
		spin:              newSpinner(),
		help:              help.New(),
		keys:              newChatKeyMap(),
		mdStyle:           mdStyle,
		client:            client,
		logo:              renderLogoHeader(),
		transcriptDir:     cfg.TranscriptDir,
		baseURL:           cfg.BaseURL,
		provider:          cfg.Provider,
		recallIdx:         -1,
	}
	m.initCmd = sparkleTick(0)
	openAIClient, canCheckOpenAI := client.(*openai.Client)
	if cfg.CustomCatalog() && !config.CustomEndpoint(cfg.BaseURL) && canCheckOpenAI {
		// Config-supplied models show in the picker only once the API
		// confirms they exist; the check starts now, or from handleKeyCheck
		// when key entry has to supply the client first. The catalog itself
		// is active immediately — default_model may open a chat that needs
		// its rates and reasoning defaults before the check lands.
		// Custom endpoints skip the check: their model names are not
		// OpenAI's to vouch for.
		m.pendingCatalog = cfg.Catalog()
		m.catalog = m.pendingCatalog
		m.rates = ratesFrom(m.pendingCatalog)
		m.keyNotice = "checking model availability…"
		m.initCmd = tea.Batch(m.initCmd, checkModels(openAIClient))
	} else {
		if cfg.CustomCatalog() {
			m.setCatalog(cfg.Catalog())
		} else {
			m.builtinCatalog = true
			m.refreshBuiltinCatalog()
		}
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
	if client == nil && !keystore.Any() && !cfg.HasCustomEndpoint() && m.keyNotice == "" {
		m.keyNotice = "no API keys configured — ctrl+k opens the key manager"
	}
	return m
}

// newClient builds a client for the global endpoint with the given key,
// honoring a config base_url. Used when key entry replaces the client.
func (m Model) newClient(key string) openai.ChatClient {
	if m.provider == "anthropic" {
		if m.baseURL != "" {
			return provideranthropic.New(key, provideranthropic.WithBaseURL(m.baseURL))
		}
		return provideranthropic.New(key)
	}
	if m.provider == "ollama" {
		return ollama.New(m.baseURL)
	}
	if m.baseURL != "" {
		return openai.New(key, openai.WithBaseURL(m.baseURL))
	}
	return openai.New(key)
}

// clientFor returns the client to talk to a model with: the default client,
// unless the model's catalog entry names its own base_url, in which case a
// client for that endpoint is built (once) with the same key.
func (m *Model) clientFor(id string) openai.ChatClient {
	cm := m.modelConfig(id)
	url := cm.BaseURL
	globalProvider := m.provider
	if globalProvider == "" {
		globalProvider = "openai"
	}
	provider := cm.Provider
	if provider == "" {
		provider = globalProvider
	}
	if (url == "" || url == m.baseURL) && provider == globalProvider {
		return m.client
	}
	cacheKey := provider + "\x00" + url
	if c, ok := m.clients[cacheKey]; ok {
		return c
	}
	if m.clients == nil {
		m.clients = make(map[string]openai.ChatClient)
	}
	var c openai.ChatClient
	if provider == "anthropic" {
		key := keystore.Resolve(keystore.Anthropic)
		if key == "" {
			return nil
		}
		if url != "" {
			c = provideranthropic.New(key, provideranthropic.WithBaseURL(url))
		} else {
			c = provideranthropic.New(key)
		}
	} else if provider == "ollama" {
		c = ollama.New(url)
	} else {
		key := keystore.Resolve(keystore.OpenAI)
		if url != "" {
			c = openai.New(key, openai.WithBaseURL(url))
		} else {
			c = openai.New(key)
		}
	}
	m.clients[cacheKey] = c
	return c
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

	case modelsCheckMsg:
		return m.handleModelsCheck(msg)

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
