package ui

// The chat screen: a scrollable conversation viewport above a multi-line
// input, with a cost/token summary in the header and key help in the bottom
// bar. Ctrl+p temporarily repurposes the input as a system prompt editor.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"

	"github.com/dicedatalore/oolong/internal/clipboard"
	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/mathfmt"
	"github.com/dicedatalore/oolong/internal/openai"
)

func newChatInput() textarea.Model {
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
	inputStyles.Focused.Prompt = inputStyles.Focused.Prompt.Foreground(peach)
	inputStyles.Cursor.Color = peach
	input.SetStyles(inputStyles)
	return input
}

// openChat switches to the chat screen talking to model id. An in-progress
// conversation (and message draft) carries over, so picking another model
// mid-chat continues where it left off; ctrl+n starts over.
func (m Model) openChat(id string) (tea.Model, tea.Cmd) {
	m.chosen = id
	m.state = stateChat
	m.keyNotice = ""
	m.vp = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(m.height))
	m.layoutChat()
	m.vp.GotoBottom()
	return m, m.input.Focus()
}

// resetChat clears per-session chat state: the transcript, system prompt,
// token counters, notices, and pending attachments.
func (m *Model) resetChat() {
	m.messages = nil
	m.msgCache = nil
	m.systemPrompt = ""
	m.errText = ""
	m.chatNotice = ""
	m.inputTokens = 0
	m.outputTokens = 0
	m.costUSD = 0
	m.pendingImages = nil
}

// layoutChat resizes the viewport and input to fill the window and rebuilds
// the markdown renderer for the new width. Called whenever the window,
// input height, or the rows around the input change.
func (m *Model) layoutChat() {
	contentWidth := m.width - pageStyle.GetHorizontalFrameSize()
	contentHeight := m.height - pageStyle.GetVerticalFrameSize()
	headerHeight := 1 + headerBarStyle.GetVerticalFrameSize()
	// Size the input before reading its height: with DynamicHeight the
	// textarea only recomputes its height when its width is set, so the
	// stale default would leak into the viewport size below.
	m.input.SetWidth(contentWidth - inputRowStyle.GetHorizontalFrameSize() - 4)
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

	// Render markdown without glamour's document margin: the reply sits
	// in a left-bordered block like user messages, and the block provides
	// the indentation. Wrap to the block's inner width (border + padding
	// take 2 of the 4 columns the block is inset by).
	cfg := styles.DarkStyleConfig
	if m.mdStyle == styles.LightStyle {
		cfg = styles.LightStyleConfig
	}
	cfg.Document.Margin = new(uint)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(contentWidth-6),
	)
	if err == nil {
		m.renderer = renderer
	}
	// Rendered output only depends on the content width, so the cache
	// survives layout changes that don't alter it (help toggle, input
	// growth, notices).
	if contentWidth != m.cacheWidth {
		m.cacheWidth = contentWidth
		m.msgCache = nil
	}
	m.vp.SetContent(m.conversationView())
}

// conversationView renders the whole transcript: user messages in bordered
// blocks, assistant messages as glamour-rendered markdown. Completed
// messages come from msgCache, so the per-delta cost while streaming stays
// constant instead of growing with the transcript.
func (m *Model) conversationView() string {
	if len(m.messages) == 0 {
		return helpStyle.Render("\n  Say something to get started.")
	}
	// The transcript can shrink (regenerate drops the last reply, ctrl+n
	// clears it); stale tail entries must not survive.
	if len(m.msgCache) > len(m.messages) {
		m.msgCache = m.msgCache[:len(m.messages)]
	}
	for i := len(m.msgCache); i < len(m.messages); i++ {
		m.msgCache = append(m.msgCache, m.renderMessage(m.messages[i]))
	}
	if m.streaming {
		last := len(m.messages) - 1
		m.msgCache[last] = m.renderMessage(m.messages[last])
	}
	var b strings.Builder
	for _, block := range m.msgCache {
		b.WriteString(block)
	}
	return b.String()
}

// renderMessage renders one message to its on-screen block.
func (m *Model) renderMessage(msg openai.Message) string {
	if msg.Role == "user" {
		var block strings.Builder
		block.WriteString(userLabelStyle.Render("You"))
		if n := len(msg.Images); n > 0 {
			block.WriteString("\n" + helpStyle.Render(imageLabel(n)))
		}
		if msg.Content != "" {
			block.WriteString("\n" + msg.Content)
		}
		return userBlockStyle.Width(m.vp.Width()-4).Render(block.String()) + "\n\n"
	}
	rendered := msg.Content
	if m.renderer != nil {
		if out, err := m.renderer.Render(mathfmt.Render(msg.Content)); err == nil {
			// Glamour pads its output with blank lines; the block
			// provides the spacing instead.
			rendered = strings.Trim(out, "\n")
		}
	}
	label := msg.Model
	if label == "" {
		label = m.chosen
	}
	return botBlockStyle.Width(m.vp.Width()-4).Render(
		botLabelStyle.Render(label)+"\n"+rendered) + "\n\n"
}

func (m Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.editingSystem {
			return m.updateSystemPrompt(key)
		}
		switch key.String() {
		case "esc":
			// While a response is in flight, esc stops the stream and
			// returns to the input bar; otherwise it goes to the model
			// picker with the conversation kept.
			if m.waiting {
				m.settleStreamEstimate()
				m.finishStream()
				return m, nil
			}
			m.state = statePicker
			m.input.Blur()
			if len(m.messages) > 0 {
				m.keyNotice = "chat kept — pick a model to continue, ctrl+n starts fresh"
			}
			m.sparkleTag++
			return m, sparkleTick(m.sparkleTag)
		case "ctrl+n":
			if m.waiting {
				return m, nil
			}
			m.resetChat()
			m.help.ShowAll = false
			m.layoutChat()
			m.chatNotice = "new chat"
			return m, nil
		case "ctrl+t":
			// Cycle the session's reasoning effort: model default → none →
			// minimal → low → medium → high → back to model default.
			if m.waiting {
				return m, nil
			}
			m.effortOverride = nextEffort(m.effortOverride)
			if m.effortOverride == "" {
				m.chatNotice = "reasoning effort: model default"
			} else {
				m.chatNotice = "reasoning effort: " + m.effortOverride
			}
			m.help.ShowAll = false
			m.layoutChat()
			return m, nil
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
		case "ctrl+y":
			if m.waiting {
				return m, nil
			}
			m.help.ShowAll = false
			if reply, ok := m.lastMessage("assistant"); !ok {
				m.chatNotice = "nothing to copy yet"
			} else if err := clipboard.WriteText(reply); err != nil {
				m.chatNotice = "copy failed: " + err.Error()
			} else {
				m.chatNotice = "copied last reply"
			}
			m.layoutChat()
			return m, nil
		case "ctrl+r":
			// Ask the current model again: drop the last reply and
			// re-stream it. After a failed request (no reply arrived)
			// the transcript ends with the user message; just retry.
			if m.waiting || len(m.messages) == 0 {
				return m, nil
			}
			if m.messages[len(m.messages)-1].Role == "assistant" {
				m.messages = m.messages[:len(m.messages)-1]
			}
			m.errText = ""
			m.chatNotice = ""
			m.help.ShowAll = false
			m.layoutChat()
			m.vp.GotoBottom()
			m.waiting = true
			return m, tea.Batch(m.spin.Tick, m.startStream())
		case "up":
			// With an empty composer, up recalls the last sent message for
			// editing; otherwise up moves the cursor in the textarea below.
			if !m.waiting && strings.TrimSpace(m.input.Value()) == "" {
				if sent, ok := m.lastMessage("user"); ok {
					prevHeight := m.input.Height()
					m.input.SetValue(sent)
					if m.input.Height() != prevHeight {
						m.layoutChat()
						m.vp.GotoBottom()
					}
					return m, nil
				}
			}
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

	// Non-key messages (mouse wheel, blink ticks, …) go to both widgets.
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

// nextEffort steps the ctrl+t cycle one level: through config.Efforts in
// order, with "" (defer to the model's configured default) on either end.
func nextEffort(cur string) string {
	for i, e := range config.Efforts {
		if e == cur {
			if i == len(config.Efforts)-1 {
				return ""
			}
			return config.Efforts[i+1]
		}
	}
	return config.Efforts[0]
}

// lastMessage returns the content of the most recent message with the
// given role.
func (m Model) lastMessage(role string) (string, bool) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == role {
			return m.messages[i].Content, true
		}
	}
	return "", false
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

// viewChat stacks the header, conversation viewport, input area, and bottom
// bar into the full chat page.
func (m Model) viewChat() string {
	// While streaming, the header includes a live local estimate of the
	// in-flight request; the server's usage report replaces it on
	// completion.
	in, out, usd := m.inputTokens, m.outputTokens, m.costUSD
	if ein, eout := m.streamEstimate(); ein > 0 || eout > 0 {
		in += ein
		out += eout
		if r, ok := m.rates[m.chosen]; ok {
			usd += float64(ein)/1e6*r.input + float64(eout)/1e6*r.output
		}
	}
	cost := fmt.Sprintf("~$%.4f • %s in / %s out",
		usd, formatTokens(in), formatTokens(out))
	if eff := m.effectiveEffort(); eff != "" {
		cost += " • effort: " + eff
	}
	if m.systemPrompt != "" {
		cost += " • system prompt"
	}
	header := headerBarStyle.Render(headerStyle.Render(m.chosen) + helpStyle.Render("  "+cost))

	// The bottom bar shows one thing at a time, in order of urgency:
	// error > spinner > notice > system prompt hints > key help.
	bottomBar := m.help.View(m.keys)
	if m.editingSystem {
		bottomBar = helpStyle.Render("enter save • esc cancel • empty prompt clears")
	}
	if m.chatNotice != "" {
		bottomBar = helpStyle.Render(m.chatNotice)
	}
	if m.waiting {
		label := " thinking…"
		if m.streaming {
			label = " streaming…"
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
	return pageStyle.Render(header + "\n" + m.vp.View() + "\n" +
		inputArea + "\n" + bottomBar)
}
