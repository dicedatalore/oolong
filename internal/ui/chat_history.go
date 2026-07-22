package ui

import "slices"

// recallActive reports whether the composer holds an unedited recalled
// message. Any edit makes the value differ from recallText, which drops the
// composer out of history stepping without needing an explicit reset.
func (m Model) recallActive() bool {
	return m.recallIdx >= 0 && m.input.Value() == m.recallText
}

// recallMessage loads the sent message at idx into the composer, including
// its attachments.
func (m *Model) recallMessage(idx int) {
	msg := m.messages[idx]
	m.recallIdx = idx
	m.recallText = msg.Content
	m.input.SetValue(msg.Content)
	m.pendingImages = slices.Clone(msg.Images)
	m.pendingFiles = slices.Clone(msg.Files)
	m.layoutChat()
	m.vp.GotoBottom()
}

// exitRecall restores the composer to the state before recall started: empty
// text, plus any attachments that were already pending.
func (m *Model) exitRecall() {
	m.input.SetValue("")
	m.pendingImages = m.recallSavedImages
	m.pendingFiles = m.recallSavedFiles
	m.clearRecall()
	m.layoutChat()
	m.vp.GotoBottom()
}

func (m *Model) clearRecall() {
	m.recallIdx = -1
	m.recallText = ""
	m.recallSavedImages = nil
	m.recallSavedFiles = nil
}

// prevUserMessage returns the index of the last user message before i, -1
// when there is none.
func (m Model) prevUserMessage(i int) int {
	for i--; i >= 0; i-- {
		if m.messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

// nextUserMessage returns the index of the first user message after i, -1
// when there is none.
func (m Model) nextUserMessage(i int) int {
	for i++; i < len(m.messages); i++ {
		if m.messages[i].Role == "user" {
			return i
		}
	}
	return -1
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
