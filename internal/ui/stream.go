package ui

// Streaming a reply is a loop between a background goroutine and Update:
// startStream launches openai.StreamChat on a goroutine that writes events
// to a channel, and readStream is a tea.Cmd that receives ONE event and
// delivers it to Update as a streamEventMsg. handleStreamEvent applies the
// event and issues readStream again, so events arrive one message at a time
// and the UI stays responsive while text streams in.
//
// Stopping (esc/ctrl+c) cancels the stream's context; the goroutine notices,
// closes the channel, and the loop ends.

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/openai"
)

type streamEventMsg openai.StreamEvent

// startStream kicks off a streaming completion for the current transcript
// (prefixed with the system prompt, if set) and returns the command that
// waits for the first event.
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
// re-issued from handleStreamEvent after each delta so events arrive one
// per message.
func readStream(ch <-chan openai.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return streamEventMsg(ev)
	}
}

// finishStream tears down the in-flight stream, if any: it cancels the
// request and clears the streaming flags. Safe to call when nothing is
// streaming.
func (m *Model) finishStream() {
	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}
	m.stream = nil
	m.streaming = false
	m.waiting = false
}

// handleStreamEvent folds one stream event into the chat: append a delta to
// the assistant message, or finish up on done/error.
func (m Model) handleStreamEvent(msg streamEventMsg) (tea.Model, tea.Cmd) {
	if m.state != stateChat || m.stream == nil {
		// Stale event from a stream that was already stopped.
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
		// First delta: append the assistant message the deltas build up in.
		if !m.streaming {
			m.streaming = true
			m.messages = append(m.messages, openai.Message{Role: "assistant"})
		}
		m.messages[len(m.messages)-1].Content += msg.Delta
		// Keep following the newest text only if the user hasn't scrolled up.
		atBottom := m.vp.AtBottom()
		m.vp.SetContent(m.conversationView())
		if atBottom {
			m.vp.GotoBottom()
		}
		return m, readStream(m.stream)
	}
}
