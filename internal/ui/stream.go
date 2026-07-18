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
	chars := 0
	for _, msg := range history {
		chars += len(msg.Content)
	}
	m.estInputTokens = estimateTokens(chars)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan openai.StreamEvent)
	m.stream = ch
	m.cancelStream = cancel
	m.streaming = false
	opts := openai.Options{
		ReasoningEffort: m.effectiveEffort(),
		Verbosity:       m.modelConfig(m.chosen).Verbosity,
	}
	go m.client.StreamChat(ctx, m.chosen, history, opts, ch)
	return readStream(ch)
}

// effectiveEffort resolves the reasoning effort for the next request: the
// session override wins over the model's configured default; "" omits the
// parameter, leaving the server default.
func (m Model) effectiveEffort() string {
	if m.effortOverride != "" {
		return m.effortOverride
	}
	return m.modelConfig(m.chosen).ReasoningEffort
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

// estimateTokens approximates a token count from a character count with
// the usual ~4 chars/token heuristic. Used for the in-flight numbers in
// the header; real usage from the server replaces it when a response
// completes.
func estimateTokens(chars int) int {
	return chars / 4
}

// streamEstimate returns the estimated usage of the in-flight request:
// the input that was sent and the output streamed so far.
func (m Model) streamEstimate() (in, out int) {
	if !m.waiting {
		return 0, 0
	}
	in = m.estInputTokens
	if m.streaming {
		out = estimateTokens(len(m.messages[len(m.messages)-1].Content))
	}
	return in, out
}

// settleStreamEstimate folds the in-flight estimate into the session
// totals. Called when a stream is stopped early: the request still
// incurred cost, but its usage report will never arrive.
func (m *Model) settleStreamEstimate() {
	in, out := m.streamEstimate()
	m.inputTokens += in
	m.outputTokens += out
	if r, ok := m.rates[m.chosen]; ok {
		m.costUSD += float64(in)/1e6*r.input + float64(out)/1e6*r.output
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
		if r, ok := m.rates[m.chosen]; ok {
			m.costUSD += float64(msg.Usage.InputTokens)/1e6*r.input +
				float64(msg.Usage.OutputTokens)/1e6*r.output
		}
		return m, nil
	default:
		// First delta: append the assistant message the deltas build up in.
		if !m.streaming {
			m.streaming = true
			m.messages = append(m.messages, openai.Message{Role: "assistant", Model: m.chosen})
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
