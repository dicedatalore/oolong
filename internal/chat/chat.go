// Package chat defines the provider-neutral conversation and streaming
// contract shared by the UI, one-shot mode, and every provider client.
package chat

import "context"

// Client is the streaming surface implemented by each provider.
type Client interface {
	StreamChat(context.Context, string, []Message, Options, chan<- StreamEvent)
}

type Message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Model   string   `json:"model,omitempty"`  // assistant messages: model that produced the reply
	Images  [][]byte `json:"images,omitempty"` // image attachments, user messages only
	Files   []File   `json:"files,omitempty"`  // text-file attachments, user messages only
}

type File struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type StreamEvent struct {
	Delta string
	Usage Usage
	Done  bool
	Err   error
}

// Options contains provider-neutral request controls. Providers ignore
// settings they do not support.
type Options struct {
	ReasoningEffort string
	Verbosity       string
}
