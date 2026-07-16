// Package ui implements the Oolong terminal user interface.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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

	"github.com/mjcadz/oolong/internal/clipboard"
	"github.com/mjcadz/oolong/internal/keystore"
	"github.com/mjcadz/oolong/internal/mathfmt"
	"github.com/mjcadz/oolong/internal/openai"
)

type sessionState int

const (
	statePicker sessionState = iota
	stateChat
	stateKeyEntry
)

var (
	// peach is the app's primary accent and purple the secondary; the logo
	// gradient in header.go runs between the same two colors.
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

	inputRowStyle  = lipgloss.NewStyle().PaddingLeft(2)
	bottomBarStyle = lipgloss.NewStyle().Padding(1, 0, 0, 2)

	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(peach)
	botLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(purple)
	// User messages render in a peach-bordered block so they stand apart
	// from the model's markdown output in long transcripts.
	userBlockStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(peach).
			PaddingLeft(1)
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
)

// USD per 1M tokens. https://openai.com/api/pricing
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
	Send      key.Binding
	NewLine   key.Binding
	Paste     key.Binding
	Scroll    key.Binding
	Jump      key.Binding
	SysPrompt key.Binding
	Save      key.Binding
	Stop      key.Binding
	Back      key.Binding
	Quit      key.Binding
	Help      key.Binding
}

// ShortHelp keeps the most used keys visible; everything else lives in the
// full help behind "?".
func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.NewLine, k.Back, k.Help}
}

func (k chatKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.NewLine, k.Paste, k.Scroll},
		{k.Jump, k.Stop, k.SysPrompt, k.Save},
		{k.Back, k.Quit, k.Help},
	}
}

func newChatKeyMap() chatKeyMap {
	return chatKeyMap{
		Send:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		NewLine:   key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line")),
		Paste:     key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste")),
		Scroll:    key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		Jump:      key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", "top/bottom")),
		SysPrompt: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "system prompt")),
		Save:      key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save chat")),
		Stop:      key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc/ctrl+c", "stop")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "change model")),
		Quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
	}
}

type streamEventMsg openai.StreamEvent

// sparkleMsg re-randomizes the banner's stripe row while the picker is
// showing. The tag ties a tick to the picker visit that scheduled it, so a
// stale tick from a previous visit can't start a second tick loop.
type sparkleMsg struct{ tag int }

func sparkleTick(tag int) tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return sparkleMsg{tag: tag}
	})
}

type keyCheckMsg struct {
	key string
	err error
}

type Model struct {
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
	messages []openai.Message
	waiting  bool
	errText  string

	stream        <-chan openai.StreamEvent
	cancelStream  context.CancelFunc
	streaming     bool     // an in-progress assistant message is the last element of messages
	pendingImages [][]byte // pasted images sent with the next message

	systemPrompt  string
	editingSystem bool   // the input textarea is editing the system prompt
	draft         string // message draft stashed while editing the system prompt
	chatNotice    string // transient status line in the chat bottom bar

	inputTokens  int
	outputTokens int

	keyInput      textinput.Model
	keyErr        string
	keyNotice     string
	keyValidating bool

	mdStyle    string
	client     *openai.Client
	logo       string
	sparkleTag int
}

// pickerLogo returns the banner shown above the model picker, or "" when the
// window is too narrow for the wordmark to fit without wrapping.
func (m Model) pickerLogo() string {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	if contentWidth < lipgloss.Width(logoRows[0]) {
		return ""
	}
	return m.logo
}

func New(client *openai.Client, mdStyle string) Model {
	items := []list.Item{
		modelItem{id: "gpt-5.6-luna", desc: "For cost-sensitive workloads"},
		modelItem{id: "gpt-5.6-terra", desc: "Balances intelligence and cost"},
		modelItem{id: "gpt-5.6-sol", desc: "For complex professional work"},
	}
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(peach).BorderForeground(peach)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(peachDim).BorderForeground(peach)
	picker := list.New(items, delegate, 0, 0)
	picker.Title = "Pick a model"
	picker.Styles.Title = headerStyle
	picker.Styles.ActivePaginationDot = picker.Styles.ActivePaginationDot.Foreground(peach)
	picker.SetShowStatusBar(false)
	// Help renders separately in View so the list block can be centered
	// while the command bar stays pinned to the bottom of the window.
	picker.SetShowHelp(false)
	// The picker has no full-help view, so drop "?" from its command bar.
	// A keyless binding stays disabled even when the list re-evaluates its
	// keybindings; SetEnabled(false) alone would be undone on every update.
	picker.KeyMap.ShowFullHelp = key.NewBinding()
	picker.KeyMap.CloseFullHelp = key.NewBinding()
	picker.KeyMap.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit"))
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
	spin.Style = lipgloss.NewStyle().Foreground(peach)

	keyInput := textinput.New()
	keyInput.Placeholder = "sk-..."
	keyInput.EchoMode = textinput.EchoPassword
	keyInput.EchoCharacter = '•'
	keyInput.CharLimit = 0

	state := statePicker
	initCmd := sparkleTick(0)
	if client == nil {
		state = stateKeyEntry
		initCmd = keyInput.Focus()
	}

	return Model{
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
		logo:     renderLogoHeader(),
	}
}

func (m Model) Init() tea.Cmd {
	return m.initCmd
}

func (m *Model) layoutChat() {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	contentHeight := m.height - pageStyle.GetVerticalFrameSize()
	headerHeight := 1 + headerBarStyle.GetVerticalFrameSize()
	inputHeight := m.input.Height()
	if len(m.pendingImages) > 0 {
		inputHeight++ // attachment indicator line above the input
	}
	if m.editingSystem {
		inputHeight++ // system prompt indicator line above the input
	}
	m.help.SetWidth(contentWidth - bottomBarStyle.GetHorizontalFrameSize())
	helpHeight := 1
	if m.help.ShowAll {
		helpHeight = lipgloss.Height(m.help.View(m.keys))
	}
	bottomBarHeight := helpHeight + bottomBarStyle.GetVerticalFrameSize()
	m.vp.SetWidth(contentWidth)
	m.vp.SetHeight(contentHeight - headerHeight - inputHeight - bottomBarHeight)
	m.input.SetWidth(contentWidth - inputRowStyle.GetHorizontalFrameSize() - 4)

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.mdStyle),
		glamour.WithWordWrap(contentWidth-2),
	)
	if err == nil {
		m.renderer = renderer
	}
	m.vp.SetContent(m.conversationView())
}

func (m *Model) conversationView() string {
	if len(m.messages) == 0 {
		return helpStyle.Render("\n  Say something to get started.")
	}
	var b strings.Builder
	for _, msg := range m.messages {
		if msg.Role == "user" {
			var block strings.Builder
			block.WriteString(userLabelStyle.Render("You"))
			if n := len(msg.Images); n > 0 {
				block.WriteString("\n" + helpStyle.Render(imageLabel(n)))
			}
			if msg.Content != "" {
				block.WriteString("\n" + msg.Content)
			}
			b.WriteString(userBlockStyle.Width(m.vp.Width()-4).Render(block.String()) + "\n\n")
			continue
		}
		b.WriteString(botLabelStyle.Render(m.chosen) + "\n")
		rendered := msg.Content
		if m.renderer != nil {
			if out, err := m.renderer.Render(mathfmt.Render(msg.Content)); err == nil {
				rendered = out
			}
		}
		b.WriteString(rendered + "\n")
	}
	return b.String()
}

func (m *Model) startStream() tea.Cmd {
	history := make([]openai.Message, 0, len(m.messages)+1)
	if m.systemPrompt != "" {
		history = append(history, openai.Message{Role: "system", Content: m.systemPrompt})
	}
	history = append(history, m.messages...)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan openai.StreamEvent)
	m.stream = ch
	m.cancelStream = cancel
	m.streaming = false
	go m.client.StreamChat(ctx, m.chosen, history, ch)
	return readStream(ch)
}

// readStream waits for the next event from the in-flight stream. It is
// re-issued from Update after each delta so events arrive one per message.
func readStream(ch <-chan openai.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return streamEventMsg(ev)
	}
}

func (m *Model) finishStream() {
	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}
	m.stream = nil
	m.streaming = false
	m.waiting = false
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
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

	case sparkleMsg:
		if m.state != statePicker || msg.tag != m.sparkleTag {
			return m, nil
		}
		m.logo = renderLogoHeader()
		return m, sparkleTick(msg.tag)

	case streamEventMsg:
		if m.state != stateChat || m.stream == nil {
			return m, nil
		}
		switch {
		case msg.Err != nil:
			m.finishStream()
			m.errText = msg.Err.Error()
			return m, nil
		case msg.Done:
			m.finishStream()
			m.inputTokens += msg.Usage.InputTokens
			m.outputTokens += msg.Usage.OutputTokens
			return m, nil
		default:
			if !m.streaming {
				m.streaming = true
				m.messages = append(m.messages, openai.Message{Role: "assistant"})
			}
			m.messages[len(m.messages)-1].Content += msg.Delta
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
		m.client = openai.New(msg.key)
		if err := keystore.Set(msg.key); err != nil {
			m.keyNotice = "couldn't save to keychain; key active for this session only"
		} else {
			m.keyNotice = "key saved to OS keychain"
		}
		m.keyInput.SetValue("")
		m.keyInput.Blur()
		m.keyErr = ""
		m.state = statePicker
		m.sparkleTag++
		return m, sparkleTick(m.sparkleTag)

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

func (m Model) updateKeyEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.keyValidating {
			if key.String() == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch key.String() {
		case "esc", "ctrl+c":
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
				return keyCheckMsg{key: k, err: openai.ValidateKey(k)}
			})
		}
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.picker.FilterState() != list.Filtering {
		switch key.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "ctrl+k":
			keystore.Delete()
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

func (m Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.editingSystem {
			return m.updateSystemPrompt(key)
		}
		switch key.String() {
		case "esc":
			// While a response is in flight, esc stops the stream and
			// returns to the input bar; otherwise it leaves the chat.
			if m.waiting {
				m.finishStream()
				return m, nil
			}
			m.finishStream()
			m.state = statePicker
			m.messages = nil
			m.errText = ""
			m.chatNotice = ""
			m.inputTokens = 0
			m.outputTokens = 0
			m.pendingImages = nil
			m.input.Blur()
			m.sparkleTag++
			return m, sparkleTick(m.sparkleTag)
		case "ctrl+p":
			if m.waiting {
				return m, nil
			}
			m.editingSystem = true
			m.draft = m.input.Value()
			m.input.SetValue(m.systemPrompt)
			m.input.Placeholder = "You are a helpful assistant…"
			m.chatNotice = ""
			m.help.ShowAll = false
			m.layoutChat()
			return m, nil
		case "ctrl+s":
			m.help.ShowAll = false
			if len(m.messages) == 0 {
				m.chatNotice = "nothing to save yet"
			} else if name, err := m.saveTranscript(); err != nil {
				m.errText = "save failed: " + err.Error()
			} else {
				m.chatNotice = "saved " + name
			}
			m.layoutChat()
			return m, nil
		case "home":
			m.vp.GotoTop()
			return m, nil
		case "end":
			m.vp.GotoBottom()
			return m, nil
		case "?":
			// "?" toggles the full help, but only when the input is empty;
			// otherwise it is just a character being typed.
			if !m.waiting && strings.TrimSpace(m.input.Value()) == "" {
				m.help.ShowAll = !m.help.ShowAll
				m.chatNotice = ""
				m.layoutChat()
				return m, nil
			}
		case "ctrl+v":
			// An image on the clipboard becomes an attachment; otherwise
			// fall through and let the textarea paste it as text.
			if img, err := clipboard.Image(); err == nil && len(img) > 0 {
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
			m.chatNotice = ""
			m.help.ShowAll = false
			m.messages = append(m.messages, openai.Message{Role: "user", Content: text, Images: m.pendingImages})
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

// updateSystemPrompt handles keys while the input textarea is repurposed as
// the system prompt editor: enter saves, esc cancels, and the stashed message
// draft is restored either way.
func (m Model) updateSystemPrompt(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.exitSystemPrompt()
		return m, nil
	case "enter":
		m.systemPrompt = strings.TrimSpace(m.input.Value())
		if m.systemPrompt == "" {
			m.chatNotice = "system prompt cleared"
		} else {
			m.chatNotice = "system prompt set"
		}
		m.exitSystemPrompt()
		return m, nil
	}
	var cmd tea.Cmd
	prevHeight := m.input.Height()
	m.input, cmd = m.input.Update(key)
	if m.input.Height() != prevHeight {
		m.layoutChat()
	}
	return m, cmd
}

func (m *Model) exitSystemPrompt() {
	m.editingSystem = false
	m.input.SetValue(m.draft)
	m.draft = ""
	m.input.Placeholder = "Send a message…"
	m.help.ShowAll = false
	m.layoutChat()
}

// saveTranscript writes the conversation as markdown to a timestamped file in
// the working directory and returns the file name.
func (m Model) saveTranscript() (string, error) {
	name := fmt.Sprintf("oolong-chat-%s.md", time.Now().Format("2006-01-02-150405"))
	return name, os.WriteFile(name, []byte(m.transcriptMarkdown()), 0o644)
}

func (m Model) transcriptMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Oolong chat — %s\n\n_%s_\n\n", m.chosen, time.Now().Format("2006-01-02 15:04"))
	if m.systemPrompt != "" {
		fmt.Fprintf(&b, "**System prompt:** %s\n\n", m.systemPrompt)
	}
	for _, msg := range m.messages {
		if msg.Role == "user" {
			b.WriteString("## You\n\n")
			if n := len(msg.Images); n > 0 {
				fmt.Fprintf(&b, "_%s_\n\n", imageLabel(n))
			}
			if msg.Content != "" {
				fmt.Fprintf(&b, "%s\n\n", msg.Content)
			}
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", m.chosen, msg.Content)
	}
	return b.String()
}

func (m Model) sessionCost() float64 {
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

func (m Model) View() tea.View {
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
		contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
		contentHeight := m.height - pageStyle.GetVerticalFrameSize()

		// The list pads itself to its set height; trim that so the block
		// centers on its actual content.
		view := strings.TrimRight(m.picker.View(), " \n")
		if logo := m.pickerLogo(); logo != "" {
			view = logo + "\n\n" + view
		}
		// Pad every line to the block's width so Place centers the block as
		// a unit; otherwise it centers each line individually and the list's
		// left edge no longer lines up.
		view = lipgloss.NewStyle().Width(lipgloss.Width(view)).Render(view)

		bottomBar := m.help.View(m.picker)
		if m.keyNotice != "" {
			bottomBar = helpStyle.Render(m.keyNotice) + "\n" + bottomBar
		}
		bottomBarHeight := lipgloss.Height(bottomBar)

		centered := lipgloss.Place(contentWidth, contentHeight-bottomBarHeight,
			lipgloss.Center, lipgloss.Center, view)
		v.SetContent(pageStyle.Render(centered + "\n" +
			lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, bottomBar)))
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
		if m.systemPrompt != "" {
			cost += " • system prompt"
		}
		header := headerBarStyle.Render(headerStyle.Render(m.chosen) + helpStyle.Render("  "+cost))

		bottomBar := m.help.View(m.keys)
		if m.editingSystem {
			bottomBar = helpStyle.Render("enter save • esc cancel • empty prompt clears")
		}
		if m.chatNotice != "" {
			bottomBar = helpStyle.Render(m.chatNotice)
		}
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
		if m.editingSystem {
			inputArea = inputRowStyle.Render(botLabelStyle.Render("System prompt")) +
				"\n" + inputArea
		}
		if n := len(m.pendingImages); n > 0 {
			inputArea = inputRowStyle.Render(helpStyle.Render(imageLabel(n)+" — sent with next message")) +
				"\n" + inputArea
		}
		v.SetContent(pageStyle.Render(header + "\n" + m.vp.View() + "\n" +
			inputArea + "\n" + bottomBar))
	}
	return v
}
