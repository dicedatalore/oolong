package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
)

type sessionState int

const (
	statePicker sessionState = iota
	stateChat
	stateKeyEntry
)

var (
	pageStyle = lipgloss.NewStyle().Padding(1, 1)

	// headerBarStyle/headerStyle/bottomBarStyle mirror the list bubble's
	// default TitleBar/Title/HelpStyle so both pages align.
	headerBarStyle = lipgloss.NewStyle().Padding(0, 0, 1, 2)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	inputRowStyle  = lipgloss.NewStyle().PaddingLeft(2)
	bottomBarStyle = lipgloss.NewStyle().Padding(1, 0, 0, 2)

	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	botLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	userTextStyle  = lipgloss.NewStyle().PaddingLeft(2)
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
)

// USD per 1M tokens. Placeholder values — update from https://openai.com/api/pricing
type modelRates struct {
	input  float64
	output float64
}

var rates = map[string]modelRates{
	"gpt-5.6-luna":  {input: 1.00, output: 6.00},
	"gpt-5.6-terra": {input: 2.50, output: 15.00},
	"gpt-5.6-sol":   {input: 5.00, output: 30.00},
}

type modelItem struct {
	id   string
	desc string
}

func (m modelItem) Title() string       { return m.id }
func (m modelItem) Description() string { return m.desc }
func (m modelItem) FilterValue() string { return m.id }

type chatKeyMap struct {
	Send    key.Binding
	NewLine key.Binding
	Paste   key.Binding
	Scroll  key.Binding
	Back    key.Binding
	Quit    key.Binding
}

func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.NewLine, k.Paste, k.Scroll, k.Back, k.Quit}
}

func (k chatKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

func newChatKeyMap() chatKeyMap {
	return chatKeyMap{
		Send:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		NewLine: key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line")),
		Paste:   key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste text/image")),
		Scroll:  key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "change model")),
		Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}

type streamEventMsg streamEvent

type keyCheckMsg struct {
	key string
	err error
}

type appModel struct {
	state  sessionState
	width  int
	height int

	picker list.Model
	chosen string

	input    textarea.Model
	vp       viewport.Model
	spin     spinner.Model
	help     help.Model
	keys     chatKeyMap
	initCmd  tea.Cmd
	renderer *glamour.TermRenderer
	messages []apiMessage
	waiting  bool
	errText  string

	stream        <-chan streamEvent
	cancelStream  context.CancelFunc
	streaming     bool     // an in-progress assistant message is the last element of messages
	pendingImages [][]byte // pasted images sent with the next message

	inputTokens  int
	outputTokens int

	keyInput      textinput.Model
	keyErr        string
	keyNotice     string
	keyValidating bool

	mdStyle string
	client  *openaiClient
}

func newAppModel(client *openaiClient, mdStyle string) appModel {
	items := []list.Item{
		modelItem{id: "gpt-5.6-luna", desc: "Fast and lightweight"},
		modelItem{id: "gpt-5.6-terra", desc: "Balanced speed and capability"},
		modelItem{id: "gpt-5.6-sol", desc: "Most capable"},
	}
	picker := list.New(items, list.NewDefaultDelegate(), 0, 0)
	picker.Title = "Pick a model"
	picker.SetShowStatusBar(false)
	picker.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "drop API key")),
		}
	}

	input := textarea.New()
	input.Placeholder = "Send a message…"
	input.CharLimit = 0
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MaxHeight = 5
	// Enter sends the message; newlines are inserted with shift+enter
	// (requires a terminal with keyboard enhancements, e.g. Kitty protocol)
	// or ctrl+j as a universal fallback.
	input.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "new line"),
	)
	// The textarea's default focused style paints the active line with a
	// background color, which reads as a gray block against the rest of
	// the view. Drop it so the input row matches the surrounding bg.
	inputStyles := input.Styles()
	inputStyles.Focused.CursorLine = inputStyles.Focused.CursorLine.Background(lipgloss.NoColor{})
	input.SetStyles(inputStyles)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	keyInput := textinput.New()
	keyInput.Placeholder = "sk-..."
	keyInput.EchoMode = textinput.EchoPassword
	keyInput.EchoCharacter = '•'
	keyInput.CharLimit = 0

	state := statePicker
	var initCmd tea.Cmd
	if client == nil {
		state = stateKeyEntry
		initCmd = keyInput.Focus()
	}

	return appModel{
		state:    state,
		picker:   picker,
		input:    input,
		spin:     spin,
		help:     help.New(),
		keys:     newChatKeyMap(),
		initCmd:  initCmd,
		keyInput: keyInput,
		mdStyle:  mdStyle,
		client:   client,
	}
}

func (m appModel) Init() tea.Cmd {
	return m.initCmd
}

func (m *appModel) layoutChat() {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	contentHeight := m.height - pageStyle.GetVerticalFrameSize()
	headerHeight := 1 + headerBarStyle.GetVerticalFrameSize()
	inputHeight := m.input.Height()
	if len(m.pendingImages) > 0 {
		inputHeight++ // attachment indicator line above the input
	}
	bottomBarHeight := 1 + bottomBarStyle.GetVerticalFrameSize()
	m.vp.SetWidth(contentWidth)
	m.vp.SetHeight(contentHeight - headerHeight - inputHeight - bottomBarHeight)
	m.input.SetWidth(contentWidth - inputRowStyle.GetHorizontalFrameSize() - 4)
	m.help.SetWidth(contentWidth - bottomBarStyle.GetHorizontalFrameSize())

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.mdStyle),
		glamour.WithWordWrap(contentWidth-2),
	)
	if err == nil {
		m.renderer = renderer
	}
	m.vp.SetContent(m.conversationView())
}

func (m *appModel) conversationView() string {
	if len(m.messages) == 0 {
		return helpStyle.Render("\n  Say something to get started.")
	}
	var b strings.Builder
	for _, msg := range m.messages {
		if msg.Role == "user" {
			b.WriteString(userLabelStyle.Render("You") + "\n")
			if n := len(msg.Images); n > 0 {
				b.WriteString(userTextStyle.Render(helpStyle.Render(imageLabel(n))) + "\n")
			}
			if msg.Content != "" {
				b.WriteString(userTextStyle.Width(m.vp.Width()-2).Render(msg.Content) + "\n")
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(botLabelStyle.Render(m.chosen) + "\n")
		rendered := msg.Content
		if m.renderer != nil {
			if out, err := m.renderer.Render(renderMath(msg.Content)); err == nil {
				rendered = out
			}
		}
		b.WriteString(rendered + "\n")
	}
	return b.String()
}

func (m *appModel) startStream() tea.Cmd {
	history := make([]apiMessage, len(m.messages))
	copy(history, m.messages)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan streamEvent)
	m.stream = ch
	m.cancelStream = cancel
	m.streaming = false
	go m.client.streamChat(ctx, m.chosen, history, ch)
	return readStream(ch)
}

// readStream waits for the next event from the in-flight stream. It is
// re-issued from Update after each delta so events arrive one per message.
func readStream(ch <-chan streamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return streamEventMsg(ev)
	}
}

func (m *appModel) finishStream() {
	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}
	m.stream = nil
	m.streaming = false
	m.waiting = false
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.SetSize(
			msg.Width-pageStyle.GetHorizontalFrameSize(),
			msg.Height-pageStyle.GetVerticalFrameSize(),
		)
		if m.state == stateChat {
			m.layoutChat()
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case streamEventMsg:
		if m.state != stateChat || m.stream == nil {
			return m, nil
		}
		switch {
		case msg.err != nil:
			m.finishStream()
			m.errText = msg.err.Error()
			return m, nil
		case msg.done:
			m.finishStream()
			m.inputTokens += msg.usage.InputTokens
			m.outputTokens += msg.usage.OutputTokens
			return m, nil
		default:
			if !m.streaming {
				m.streaming = true
				m.messages = append(m.messages, apiMessage{Role: "assistant"})
			}
			m.messages[len(m.messages)-1].Content += msg.delta
			atBottom := m.vp.AtBottom()
			m.vp.SetContent(m.conversationView())
			if atBottom {
				m.vp.GotoBottom()
			}
			return m, readStream(m.stream)
		}

	case keyCheckMsg:
		if m.state != stateKeyEntry {
			return m, nil
		}
		m.keyValidating = false
		if msg.err != nil {
			m.keyErr = msg.err.Error()
			return m, nil
		}
		m.client = newOpenAIClient(msg.key)
		if err := keyringSet(msg.key); err != nil {
			m.keyNotice = "couldn't save to keychain; key active for this session only"
		} else {
			m.keyNotice = "key saved to OS keychain"
		}
		m.keyInput.SetValue("")
		m.keyInput.Blur()
		m.keyErr = ""
		m.state = statePicker
		return m, nil

	case spinner.TickMsg:
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

func (m appModel) updateKeyEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.keyValidating {
			if key.String() == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch key.String() {
		case "esc":
			return m, tea.Quit
		case "enter":
			k := strings.TrimSpace(m.keyInput.Value())
			if k == "" {
				m.keyErr = "API key cannot be empty"
				return m, nil
			}
			m.keyValidating = true
			m.keyErr = ""
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				return keyCheckMsg{key: k, err: validateAPIKey(k)}
			})
		}
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

func (m appModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.picker.FilterState() != list.Filtering {
		switch key.String() {
		case "q", "esc":
			return m, tea.Quit
		case "ctrl+k":
			keyringDelete()
			m.client = nil
			m.keyNotice = ""
			m.keyErr = ""
			m.state = stateKeyEntry
			return m, m.keyInput.Focus()
		case "enter":
			item, ok := m.picker.SelectedItem().(modelItem)
			if !ok {
				return m, nil
			}
			m.chosen = item.id
			m.state = stateChat
			m.keyNotice = ""
			m.messages = nil
			m.errText = ""
			m.inputTokens = 0
			m.outputTokens = 0
			m.vp = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(m.height))
			m.input.SetValue("")
			m.layoutChat()
			return m, m.input.Focus()
		}
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m appModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.finishStream()
			m.state = statePicker
			m.messages = nil
			m.errText = ""
			m.inputTokens = 0
			m.outputTokens = 0
			m.pendingImages = nil
			m.input.Blur()
			return m, nil
		case "ctrl+v":
			// An image on the clipboard becomes an attachment; otherwise
			// fall through and let the textarea paste it as text.
			if img, err := clipboardImage(); err == nil && len(img) > 0 {
				m.pendingImages = append(m.pendingImages, img)
				m.layoutChat()
				return m, nil
			}
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if m.waiting || (text == "" && len(m.pendingImages) == 0) {
				return m, nil
			}
			m.errText = ""
			m.messages = append(m.messages, apiMessage{Role: "user", Content: text, Images: m.pendingImages})
			m.pendingImages = nil
			m.input.SetValue("")
			m.layoutChat()
			m.vp.SetContent(m.conversationView())
			m.vp.GotoBottom()
			m.waiting = true
			return m, tea.Batch(m.spin.Tick, m.startStream())
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		// Remaining keys belong to the textarea; up/down move the cursor
		// there, so the viewport must not also see them.
		var cmd tea.Cmd
		prevHeight := m.input.Height()
		m.input, cmd = m.input.Update(msg)
		if m.input.Height() != prevHeight {
			m.layoutChat()
			m.vp.GotoBottom()
		}
		return m, cmd
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m appModel) sessionCost() float64 {
	r, ok := rates[m.chosen]
	if !ok {
		return 0
	}
	return float64(m.inputTokens)/1e6*r.input + float64(m.outputTokens)/1e6*r.output
}

func imageLabel(n int) string {
	if n == 1 {
		return "📎 1 image"
	}
	return fmt.Sprintf("📎 %d images", n)
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func (m appModel) View() tea.View {
	v := tea.NewView("loading…")
	v.AltScreen = true
	// Report mouse events so the wheel scrolls the chat viewport; without
	// this, terminals fake wheel input as arrow keys, which now belong to
	// the textarea. Text selection needs shift-drag, as usual in TUIs.
	v.MouseMode = tea.MouseModeCellMotion
	if m.width == 0 {
		return v
	}

	switch m.state {
	case statePicker:
		view := m.picker.View()
		if m.keyNotice != "" {
			view += "\n" + bottomBarStyle.Render(helpStyle.Render(m.keyNotice))
		}
		v.SetContent(pageStyle.Render(view))
	case stateKeyEntry:
		header := headerBarStyle.Render(headerStyle.Render("OpenAI API key"))
		bottomBar := helpStyle.Render("enter: save to keychain • esc: quit")
		if m.keyValidating {
			bottomBar = m.spin.View() + helpStyle.Render("validating key…")
		} else if m.keyErr != "" {
			bottomBar = errorStyle.Render(m.keyErr)
		}
		v.SetContent(pageStyle.Render(header + "\n" +
			inputRowStyle.Render(m.keyInput.View()) + "\n" +
			bottomBarStyle.Render(bottomBar)))
	case stateChat:
		cost := fmt.Sprintf("~$%.4f • %s in / %s out",
			m.sessionCost(), formatTokens(m.inputTokens), formatTokens(m.outputTokens))
		header := headerBarStyle.Render(headerStyle.Render(m.chosen) + helpStyle.Render("  "+cost))

		bottomBar := m.help.View(m.keys)
		if m.waiting {
			label := "thinking…"
			if m.streaming {
				label = "streaming…"
			}
			bottomBar = m.spin.View() + helpStyle.Render(label)
		}
		if m.errText != "" {
			bottomBar = errorStyle.Render("error: " + m.errText)
		}
		bottomBar = bottomBarStyle.Render(bottomBar)

		inputArea := inputRowStyle.Render(m.input.View())
		if n := len(m.pendingImages); n > 0 {
			inputArea = inputRowStyle.Render(helpStyle.Render(imageLabel(n)+" — sent with next message")) +
				"\n" + inputArea
		}
		v.SetContent(pageStyle.Render(header + "\n" + m.vp.View() + "\n" +
			inputArea + "\n" + bottomBar))
	}
	return v
}

func main() {
	resetKey := flag.Bool("reset-key", false, "delete the stored OpenAI API key from the OS keychain and exit")
	flag.Parse()
	if *resetKey {
		if err := keyringDelete(); err != nil {
			fmt.Fprintf(os.Stderr, "reset-key: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Stored API key deleted.")
		return
	}

	// Query the terminal background before Bubble Tea owns the tty; doing it
	// mid-session leaks the terminal's OSC reply into the UI as garbage text.
	mdStyle := styles.LightStyle
	if termenv.HasDarkBackground() {
		mdStyle = styles.DarkStyle
	}

	var client *openaiClient
	if key := resolveAPIKey(); key != "" {
		client = newOpenAIClient(key)
	}
	app := newAppModel(client, mdStyle)
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
