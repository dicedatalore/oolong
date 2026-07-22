package ui

// The chat screen: a scrollable conversation viewport above a multi-line
// input, with a cost/token summary in the header and key help in the bottom
// bar. Ctrl+p temporarily repurposes the input as a system prompt editor.

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/dicedatalore/oolong/internal/chat"
	"github.com/dicedatalore/oolong/internal/clipboard"
	"github.com/dicedatalore/oolong/internal/mathfmt"
)

func newChatInput(theme theme) textarea.Model {
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
	inputStyles.Focused.Prompt = inputStyles.Focused.Prompt.Foreground(theme.accent)
	inputStyles.Cursor.Color = theme.accent
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
	m.errorInfo = nil
	m.showErrorDetail = false
	m.chatNotice = ""
	m.inputTokens = 0
	m.outputTokens = 0
	m.costUSD = 0
	m.pendingImages = nil
	m.pendingFiles = nil
	m.pickingFile = false
	m.retryModel = false
	m.newOutputBelow = false
	m.contextWarning = false
	m.contextPending = chat.Message{}
	m.contextResend = false
	m.contextResend = false
	m.usageEstimated = false
	m.clearEditLast()
	m.clearRecall()
}

// maxMsgWidth caps how wide conversation blocks render: full-width
// paragraphs on a wide window are hard to read. The blocks stay left-aligned
// and the viewport itself still fills the window.
const maxMsgWidth = 100

// msgWidth returns the width conversation blocks render at.
func (m Model) msgWidth() int {
	return max(1, min(m.vp.Width(), maxMsgWidth))
}

// layoutChat resizes the viewport and input to fill the window and rebuilds
// the markdown renderer for the new width. Called whenever the window,
// input height, or the rows around the input change.
func (m *Model) layoutChat() {
	page := m.pageStyle()
	contentWidth := max(1, m.width-page.GetHorizontalFrameSize())
	contentHeight := max(1, m.height-page.GetVerticalFrameSize())
	headerHeight := lipgloss.Height(m.chatHeader())
	// Size the input before reading its height: with DynamicHeight the
	// textarea only recomputes its height when its width is set, so the
	// stale default would leak into the viewport size below.
	m.input.SetWidth(max(1, contentWidth-m.theme.inputRow.GetHorizontalFrameSize()-4))
	inputHeight := m.input.Height() + m.theme.composer.GetVerticalFrameSize()
	if attachments := len(m.attachmentItems()); attachments > 0 {
		inputHeight += attachments + 1 // one row per attachment plus controls
	}
	if m.editingSystem {
		inputHeight++ // system prompt indicator line above the input
	}
	m.help.SetWidth(contentWidth)
	bottomBarHeight := lipgloss.Height(m.chatBottomBar()) + m.theme.bottomBar.GetVerticalFrameSize()
	m.vp.SetWidth(contentWidth)
	m.vp.SetHeight(max(1, contentHeight-headerHeight-inputHeight-bottomBarHeight))

	// Render markdown without glamour's document margin: the reply sits
	// in a left-bordered block like user messages, and the block provides
	// the indentation. Wrap to the block's inner width (border + padding
	// take 2 of the 4 columns the block is inset by), capped for
	// readability on wide windows.
	cfg := styles.DarkStyleConfig
	if m.mdStyle == styles.LightStyle {
		cfg = styles.LightStyleConfig
	}
	cfg.Document.Margin = new(uint)
	msgWidth := max(1, min(contentWidth, maxMsgWidth))
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(max(1, msgWidth-6)),
	)
	if err == nil {
		m.renderer = renderer
	}
	// Rendered output only depends on the (capped) message width, so the
	// cache survives layout changes that don't alter it (help toggle,
	// input growth, notices, resizes past the cap).
	if msgWidth != m.cacheWidth {
		m.cacheWidth = msgWidth
		m.msgCache = nil
	}
	m.vp.SetContent(m.conversationView())
	if m.pickingFile {
		// The picker overlays the viewport area, minus its title line.
		m.filePicker.SetHeight(max(1, m.vp.Height()-1))
	}
}

func (m *Model) renderMessageMode(msg chat.Message, live bool) string {
	if msg.Role == "user" {
		var block strings.Builder
		block.WriteString(m.theme.userLabel.Render("You"))
		if n := len(msg.Images); n > 0 {
			block.WriteString("\n" + m.theme.help.Render(imageLabel(n)))
		}
		for _, f := range msg.Files {
			block.WriteString("\n" + m.theme.help.Render("📄 "+f.Name))
		}
		if msg.Content != "" {
			block.WriteString("\n" + msg.Content)
		}
		// A small gap separates the prompt from its reply. The larger gap
		// after the assistant block separates completed exchanges.
		return m.theme.userBlock.Width(max(1, m.msgWidth()-4)).Render(block.String()) + "\n\n"
	}
	rendered := msg.Content
	// An unfinished fence changes Markdown interpretation on nearly every
	// delta. Keep it as stable plain text until the closing fence arrives.
	if m.renderer != nil && !(live && incompleteFence(msg.Content)) {
		if out, err := m.renderer.Render(mathfmt.Render(msg.Content)); err == nil {
			// Glamour pads its output with blank lines; the block
			// provides the spacing instead.
			rendered = strings.Trim(out, "\n")
			if m.theme.noColor {
				rendered = xansi.Strip(rendered)
			}
		}
	}
	label := msg.Model
	if label == "" {
		label = m.chosen
	}
	return m.theme.botBlock.Width(max(1, m.msgWidth()-4)).Render(
		m.theme.botLabel.Render(label)+"\n"+rendered) + "\n\n\n"
}

func incompleteFence(content string) bool {
	return strings.Count(content, "```")%2 == 1 || strings.Count(content, "~~~")%2 == 1
}

func (m Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.pickingFile {
		// The attach-file picker owns every message (its directory reads
		// arrive as private messages of its own).
		return m.updateFilePicker(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.contextWarning {
			return m.updateContextWarning(key)
		}
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
			if m.editIndex >= 0 {
				m.cancelEditLast()
				m.chatNotice = "edit cancelled"
				m.layoutChat()
				return m, nil
			}
			m.state = statePicker
			m.help.ShowAll = false
			m.input.Blur()
			if len(m.messages) > 0 {
				m.keyNotice = "chat kept — pick a model to continue, ctrl+n starts fresh"
			}
			m.sparkleTag++
			return m, m.sparkleCmd()
		case "ctrl+n":
			if m.waiting {
				return m, nil
			}
			m.resetChat()
			m.help.ShowAll = false
			m.layoutChat()
			m.chatNotice = "new chat"
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
			} else {
				m.chatNotice = "copied last reply"
				m.layoutChat()
				return m, tea.SetClipboard(reply)
			}
			m.layoutChat()
			return m, nil
		case "ctrl+b":
			// Copy just the last fenced code block of the last reply;
			// ctrl+y copies the whole reply.
			if m.waiting {
				return m, nil
			}
			m.help.ShowAll = false
			if reply, ok := m.lastMessage("assistant"); !ok {
				m.chatNotice = "nothing to copy yet"
			} else if code, ok := lastCodeBlock(reply); !ok {
				m.chatNotice = "no code block in the last reply"
			} else {
				m.chatNotice = "copied last code block"
				m.layoutChat()
				return m, tea.SetClipboard(code)
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
			m.clearChatError()
			m.chatNotice = ""
			m.help.ShowAll = false
			return m.beginResend()
		case "ctrl+u":
			if m.waiting || m.editIndex >= 0 {
				return m, nil
			}
			if !m.startEditLast() {
				m.chatNotice = "nothing to edit yet"
			}
			return m, nil
		case "ctrl+t":
			if m.waiting || m.editIndex >= 0 || m.prevUserMessage(len(m.messages)) < 0 {
				return m, nil
			}
			m.retryModel = true
			m.state = statePicker
			m.help.ShowAll = false
			m.input.Blur()
			m.keyNotice = ""
			m.sparkleTag++
			return m, m.sparkleCmd()
		case "ctrl+k":
			if m.waiting {
				return m, nil
			}
			return m, m.openKeyManager()
		case "ctrl+i":
			if m.errorInfo != nil {
				m.showErrorDetail = !m.showErrorDetail
				m.layoutChat()
			}
			return m, nil
		case "ctrl+d":
			if m.waiting {
				return m, nil
			}
			if name, ok := m.removeLastAttachment(); ok {
				m.chatNotice = "removed " + name
				m.layoutChat()
			}
			return m, nil
		case "alt+d":
			if m.waiting || len(m.attachmentItems()) == 0 {
				return m, nil
			}
			m.pendingImages = nil
			m.pendingFiles = nil
			m.chatNotice = "attachments cleared"
			m.layoutChat()
			return m, nil
		case "up":
			// With an empty composer, ↑ recalls the last sent message
			// (with its attachments) for editing; on an unedited recall
			// with the cursor on the first line it steps further back
			// through the sent messages. Otherwise ↑ moves the cursor in
			// the textarea below.
			if !m.waiting {
				if m.recallActive() && m.input.Line() == 0 {
					if idx := m.prevUserMessage(m.recallIdx); idx >= 0 {
						m.recallMessage(idx)
					}
					return m, nil // clamp at the oldest
				}
				if strings.TrimSpace(m.input.Value()) == "" {
					if idx := m.prevUserMessage(len(m.messages)); idx >= 0 {
						m.recallSavedImages = m.pendingImages
						m.recallSavedFiles = m.pendingFiles
						m.recallMessage(idx)
						return m, nil
					}
				}
			}
		case "down":
			// The inverse of ↑: on an unedited recall with the cursor on
			// the last line, ↓ steps toward newer sent messages, and past
			// the newest restores the pre-recall composer.
			if !m.waiting && m.recallActive() && m.input.Line() == m.input.LineCount()-1 {
				if idx := m.nextUserMessage(m.recallIdx); idx >= 0 {
					m.recallMessage(idx)
				} else {
					m.exitRecall()
				}
				return m, nil
			}
		case "home":
			m.vp.GotoTop()
			return m, nil
		case "end":
			m.vp.GotoBottom()
			m.newOutputBelow = false
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
		case "ctrl+f":
			// Attach a file from disk: the picker overlays the conversation
			// until a file is chosen or esc cancels.
			if m.waiting {
				return m, nil
			}
			m.clearChatError()
			m.chatNotice = ""
			m.help.ShowAll = false
			m.layoutChat()
			m.filePicker = newFilePicker(m.vp.Height()-1, m.theme)
			m.pickingFile = true
			return m, m.filePicker.Init()
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if m.waiting || (text == "" && len(m.pendingImages) == 0 && len(m.pendingFiles) == 0) {
				return m, nil
			}
			m.clearChatError()
			m.chatNotice = ""
			m.help.ShowAll = false
			message := chat.Message{Role: "user", Content: text, Images: m.pendingImages, Files: m.pendingFiles}
			if pct, warn := m.contextWarningFor(message); warn {
				m.contextWarning = true
				m.contextPending = message
				m.chatNotice = fmt.Sprintf("estimated context %d%% full", pct)
				m.layoutChat()
				return m, nil
			}
			return m.commitMessage(message)
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			if m.vp.AtBottom() {
				m.newOutputBelow = false
			}
			return m, cmd
		}
		// Remaining keys belong to the textarea; up/down move the cursor
		// there, so the viewport must not also see them.
		var cmd tea.Cmd
		prevHeight := m.input.Height()
		m.input, cmd = m.input.Update(msg)
		if m.recallIdx >= 0 && m.input.Value() != m.recallText {
			// The recalled message was edited; it is the draft now.
			m.clearRecall()
		}
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
	if m.vp.AtBottom() {
		m.newOutputBelow = false
	}
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) commitMessage(message chat.Message) (tea.Model, tea.Cmd) {
	if m.editIndex >= 0 {
		m.messages = m.messages[:m.editIndex]
		m.msgCache = m.msgCache[:min(len(m.msgCache), m.editIndex)]
		m.messages = append(m.messages, message)
		m.input.SetValue(m.editSavedText)
		m.pendingImages = m.editSavedImages
		m.pendingFiles = m.editSavedFiles
		m.clearEditLast()
	} else {
		m.messages = append(m.messages, message)
		m.pendingImages = nil
		m.pendingFiles = nil
		m.input.SetValue("")
	}
	m.contextWarning = false
	m.contextPending = chat.Message{}
	m.clearRecall()
	m.layoutChat()
	m.vp.SetContent(m.conversationView())
	m.vp.GotoBottom()
	m.waiting = true
	return m, m.activityCmd(m.startStream())
}

func (m Model) updateContextWarning(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.contextWarning = false
		m.contextPending = chat.Message{}
		m.contextResend = false
		m.chatNotice = "send cancelled — draft kept"
		m.layoutChat()
		return m, nil
	case "s":
		if m.contextResend {
			return m.commitResend()
		}
		message := m.contextPending
		m.chatNotice = "sending despite context warning"
		return m.commitMessage(message)
	case "a":
		if m.contextResend {
			idx := m.prevUserMessage(len(m.messages))
			if idx < 0 || (len(m.messages[idx].Images) == 0 && len(m.messages[idx].Files) == 0) {
				m.chatNotice = "no attachments to remove"
				return m, nil
			}
			m.messages[idx].Images = nil
			m.messages[idx].Files = nil
			m.msgCache = m.msgCache[:min(len(m.msgCache), idx)]
			m.chatNotice = "attachments removed — review the estimate or press s to retry"
			m.layoutChat()
			return m, nil
		}
		if len(m.contextPending.Images) == 0 && len(m.contextPending.Files) == 0 {
			m.chatNotice = "no attachments to remove"
			return m, nil
		}
		m.contextPending.Images = nil
		m.contextPending.Files = nil
		m.pendingImages = nil
		m.pendingFiles = nil
		m.chatNotice = "attachments removed — review the estimate or press s to send"
		m.layoutChat()
		return m, nil
	case "d":
		if m.dropOldestTurn() {
			pct := m.contextWarningPercent()
			m.chatNotice = fmt.Sprintf("oldest turn removed — estimated context %d%% full", pct)
		} else {
			m.chatNotice = "no older turn to remove"
		}
		m.layoutChat()
		return m, nil
	}
	return m, nil
}

func (m Model) beginResend() (tea.Model, tea.Cmd) {
	if pct, warn := m.resendContextWarning(); warn {
		m.contextWarning = true
		m.contextResend = true
		m.contextPending = chat.Message{}
		m.chatNotice = fmt.Sprintf("estimated context %d%% full", pct)
		m.layoutChat()
		return m, nil
	}
	return m.commitResend()
}

func (m Model) commitResend() (tea.Model, tea.Cmd) {
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
		m.messages = m.messages[:len(m.messages)-1]
		m.msgCache = m.msgCache[:min(len(m.msgCache), len(m.messages))]
	}
	m.contextWarning = false
	m.contextResend = false
	m.contextPending = chat.Message{}
	m.layoutChat()
	m.vp.GotoBottom()
	m.waiting = true
	return m, m.activityCmd(m.startStream())
}

func (m *Model) dropOldestTurn() bool {
	cut := -1
	for i := 1; i < len(m.messages); i++ {
		if m.messages[i].Role == "user" {
			cut = i
			break
		}
	}
	if cut < 0 || (m.editIndex >= 0 && cut > m.editIndex) {
		return false
	}
	m.messages = m.messages[cut:]
	m.msgCache = nil
	if m.editIndex >= 0 {
		m.editIndex -= cut
	}
	m.vp.SetContent(m.conversationView())
	return true
}

func (m *Model) clearChatError() {
	m.errText = ""
	m.errorInfo = nil
	m.showErrorDetail = false
}

// attachmentLabel describes the pending attachments in one line; "" when
// there are none.
func (m Model) attachmentItems() []string {
	items := make([]string, 0, len(m.pendingImages)+len(m.pendingFiles))
	for i := range m.pendingImages {
		items = append(items, fmt.Sprintf("📎 image %d", i+1))
	}
	for _, f := range m.pendingFiles {
		items = append(items, "📄 "+f.Name)
	}
	return items
}

func (m *Model) removeLastAttachment() (string, bool) {
	if n := len(m.pendingFiles); n > 0 {
		name := "📄 " + m.pendingFiles[n-1].Name
		m.pendingFiles = m.pendingFiles[:n-1]
		return name, true
	}
	if n := len(m.pendingImages); n > 0 {
		m.pendingImages = m.pendingImages[:n-1]
		return fmt.Sprintf("image %d", n), true
	}
	return "", false
}

func (m *Model) startEditLast() bool {
	idx := m.prevUserMessage(len(m.messages))
	if idx < 0 {
		return false
	}
	msg := m.messages[idx]
	m.editIndex = idx
	m.editSavedText = m.input.Value()
	m.editSavedImages = m.pendingImages
	m.editSavedFiles = m.pendingFiles
	m.input.SetValue(msg.Content)
	m.pendingImages = slices.Clone(msg.Images)
	m.pendingFiles = slices.Clone(msg.Files)
	m.clearChatError()
	m.chatNotice = "editing last prompt — enter regenerates • esc cancels"
	m.layoutChat()
	m.vp.GotoBottom()
	return true
}

func (m *Model) cancelEditLast() {
	if m.editIndex < 0 {
		return
	}
	m.input.SetValue(m.editSavedText)
	m.pendingImages = m.editSavedImages
	m.pendingFiles = m.editSavedFiles
	m.clearEditLast()
}

func (m *Model) clearEditLast() {
	m.editIndex = -1
	m.editSavedText = ""
	m.editSavedImages = nil
	m.editSavedFiles = nil
}

// retryLastWithModel enters chat on id, drops only the previous assistant
// reply, and reissues the latest user turn. The composer draft and attachments
// are untouched throughout the picker round trip.
func (m Model) retryLastWithModel(id string) (tea.Model, tea.Cmd) {
	m.retryModel = false
	next, focus := m.openChat(id)
	m = next.(Model)
	if len(m.messages) == 0 {
		return m, focus
	}
	if m.prevUserMessage(len(m.messages)) < 0 {
		return m, focus
	}
	m.clearChatError()
	m.chatNotice = "retrying with " + id
	retried, cmd := m.beginResend()
	return retried, tea.Batch(focus, cmd)
}

// chatHeader keeps the model and its quieter session metadata together in a
// compact single-row header.
func (m Model) chatHeader() string {
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
	contentWidth := max(1, m.width-m.pageStyle().GetHorizontalFrameSize()-m.theme.headerBar.GetHorizontalFrameSize())
	model := m.chosen
	if contentWidth < 34 {
		model = xansi.Truncate(model, max(1, contentWidth-2), "…")
		return m.theme.headerBar.Render(m.theme.header.Render(model))
	}
	metadata := fmt.Sprintf("$%.4f • %s in / %s out",
		usd, formatTokens(in), formatTokens(out))
	if contentWidth < 64 {
		metadata = fmt.Sprintf("%s/%s tokens", formatTokens(in), formatTokens(out))
	} else if contentWidth < 90 {
		metadata = fmt.Sprintf("%s in / %s out", formatTokens(in), formatTokens(out))
		if eff := m.modelConfig(m.chosen).ReasoningEffort; eff != "" {
			metadata += " • effort: " + eff
		}
	} else {
		if eff := m.modelConfig(m.chosen).ReasoningEffort; eff != "" {
			metadata += " • effort: " + eff
		}
		if m.systemPrompt != "" {
			metadata += " • system prompt"
		}
	}
	// The context meter turns into a warning as the window fills up.
	var ctxWarn string
	if pct, ok := m.contextUsed(); ok {
		if pct >= 80 {
			ctxWarn = m.theme.err.Render(fmt.Sprintf(" • context %d%% full", pct))
		} else if contentWidth >= 64 {
			metadata += fmt.Sprintf(" • context %d%%", pct)
		}
	}
	modelBudget := max(1, contentWidth-lipgloss.Width(metadata)-lipgloss.Width(ctxWarn)-4)
	model = xansi.Truncate(model, modelBudget, "…")
	header := m.theme.header.Render(model) + m.theme.help.Render("  "+metadata) + ctxWarn
	return m.theme.headerBar.Render(xansi.Truncate(header, contentWidth, "…"))
}

// chatComposer renders attachment/editing context and the textarea inside a
// subtle top boundary, keeping input distinct from the transcript.
func (m Model) chatComposer(contentWidth int) string {
	input := m.composerInput()
	inputArea := m.theme.inputRow.Render(input.View())
	if m.editingSystem {
		inputArea = m.theme.inputRow.Render(m.theme.botLabel.Render("System prompt")) +
			"\n" + inputArea
	}
	if items := m.attachmentItems(); len(items) > 0 {
		attachments := make([]string, 0, len(items)+1)
		for _, item := range items {
			attachments = append(attachments, m.theme.help.Render("• "+item))
		}
		attachments = append(attachments, m.theme.help.Render("ctrl+d removes last • alt+d clears all"))
		inputArea = m.theme.inputRow.Render(strings.Join(attachments, "\n")) + "\n" + inputArea
	}
	return m.theme.composer.Width(contentWidth).Render(inputArea)
}

func (m Model) composerInput() textarea.Model {
	input := m.input
	if placeholder := m.responsePlaceholder(); placeholder != "" {
		input.Placeholder = placeholder
		// Keep the colored spinner out of Placeholder: textarea slices and
		// wraps placeholder cells, which corrupts embedded ANSI styling. Its
		// prompt is rendered separately and safely preserves the fade colors.
		input.Prompt = m.activityIndicator() + " "
		input.SetWidth(input.Width())
		input.Blur()
	}
	return input
}

func (m Model) responsePlaceholder() string {
	if !m.waiting {
		return ""
	}
	label := "thinking…"
	if m.streaming {
		label = "streaming…"
	}
	placeholder := label
	if m.newOutputBelow {
		placeholder += " • new output below — end to follow"
	}
	return placeholder
}

func (m Model) chatBottomBar() string {
	// The bottom bar shows one thing at a time, in order of urgency:
	// error > notice > picker/system prompt hints > key help. Response
	// activity lives in the header so shortcuts remain available while waiting.
	bottomBar := m.help.View(m.keys)
	if m.editingSystem {
		bottomBar = m.theme.help.Render("enter save • esc cancel • empty prompt clears")
	}
	if m.pickingFile {
		bottomBar = m.theme.help.Render("enter attach • ←/→ folders • esc cancel")
	}
	if m.chatNotice != "" {
		bottomBar = m.theme.help.Render(m.chatNotice)
	}
	if m.contextWarning {
		pct := m.contextWarningPercent()
		bottomBar = m.theme.err.Render(fmt.Sprintf("estimated context %d%% full", pct)) +
			m.theme.help.Render(" — s send anyway • d drop oldest • a remove attachments • esc cancel")
	}
	if !m.waiting && m.newOutputBelow {
		bottomBar = m.theme.notice.Render("new output below — end to jump to latest")
	}
	if m.errorInfo != nil {
		bottomBar = m.theme.err.Render(m.errorInfo.summary) +
			m.theme.help.Render(" — "+m.errorInfo.hint+" • ctrl+i details")
		if m.showErrorDetail {
			bottomBar += "\n" + m.theme.help.Render(m.errorInfo.detail+" • ctrl+i hide details")
		}
	} else if m.errText != "" {
		bottomBar = m.theme.err.Render("error: " + m.errText)
	}
	return bottomBar
}
