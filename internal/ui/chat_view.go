package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dicedatalore/oolong/internal/chat"
)

// conversationView renders the whole transcript: user messages in bordered
// blocks, assistant messages as glamour-rendered markdown. Completed
// messages come from msgCache, so the per-delta cost while streaming stays
// constant instead of growing with the transcript.
func (m *Model) conversationView() string {
	if len(m.messages) == 0 {
		return m.theme.help.Render("\n  Say something to get started.")
	}
	// The transcript can shrink (regenerate drops the last reply, ctrl+n
	// clears it); stale tail entries must not survive.
	if len(m.msgCache) > len(m.messages) {
		m.msgCache = m.msgCache[:len(m.messages)]
	}
	for i := len(m.msgCache); i < len(m.messages); i++ {
		m.msgCache = append(m.msgCache, m.renderMessageMode(m.messages[i], m.streaming && i == len(m.messages)-1))
	}
	if m.streaming {
		last := len(m.messages) - 1
		m.msgCache[last] = m.renderMessageMode(m.messages[last], true)
	}
	var b strings.Builder
	for _, block := range m.msgCache {
		b.WriteString(block)
	}
	return b.String()
}

// renderMessage renders one message to its on-screen block.
func (m *Model) renderMessage(msg chat.Message) string {
	return m.renderMessageMode(msg, false)
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
	header := m.chatHeader()

	page := m.pageStyle()
	contentWidth := max(1, m.width-page.GetHorizontalFrameSize())
	inputArea := m.chatComposer(contentWidth)
	bottomBar := m.theme.bottomBar.Render(centeredBar(contentWidth, m.chatBottomBar()))

	// The attach-file picker overlays the conversation area.
	body := m.vp.View()
	if m.pickingFile {
		body = lipgloss.NewStyle().Height(m.vp.Height()).MaxHeight(m.vp.Height()).Render(
			m.theme.inputRow.Render(m.theme.botLabel.Render("Attach a file")) + "\n" + m.filePicker.View())
	}
	return page.Render(header + "\n" + body + "\n" +
		inputArea + "\n" + bottomBar)
}
