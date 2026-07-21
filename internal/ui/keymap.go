package ui

import "charm.land/bubbles/v2/key"

// chatKeyMap declares every key the chat screen responds to. A key.Binding
// pairs the actual key names with the label shown in the help bar; the
// bindings here drive the help display, while the key handling itself lives
// in updateChat.
type chatKeyMap struct {
	Send             key.Binding
	NewLine          key.Binding
	Paste            key.Binding
	Attach           key.Binding
	Copy             key.Binding
	CopyCode         key.Binding
	Regen            key.Binding
	EditLast         key.Binding
	RetryModel       key.Binding
	Recall           key.Binding
	RemoveAttachment key.Binding
	ClearAttachments key.Binding
	ErrorDetails     key.Binding
	NewChat          key.Binding
	Scroll           key.Binding
	Jump             key.Binding
	SysPrompt        key.Binding
	Save             key.Binding
	Escape           key.Binding
	Quit             key.Binding
	Help             key.Binding
}

// ShortHelp and FullHelp satisfy the help bubble's KeyMap interface, which
// Go picks up implicitly — no "implements" declaration needed.

// ShortHelp keeps the most used keys visible; everything else lives in the
// full help behind "?".
func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.NewLine, k.Escape, k.Help}
}

func (k chatKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Compose and manage attachments.
		{k.Send, k.NewLine, k.Paste, k.Attach, k.RemoveAttachment, k.ClearAttachments},
		// Continue and work with the conversation.
		{k.Recall, k.EditLast, k.Regen, k.RetryModel, k.Copy, k.CopyCode, k.ErrorDetails},
		// Navigate and manage the session.
		{k.Scroll, k.Jump, k.SysPrompt, k.Save, k.NewChat, k.Escape, k.Quit, k.Help},
	}
}

func newChatKeyMap() chatKeyMap {
	return chatKeyMap{
		Send:             key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		NewLine:          key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line")),
		Paste:            key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste")),
		Attach:           key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "attach file")),
		Copy:             key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy reply")),
		CopyCode:         key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "copy code")),
		Regen:            key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "regenerate")),
		EditLast:         key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "edit prompt")),
		RetryModel:       key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "retry model")),
		Recall:           key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "history")),
		RemoveAttachment: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "remove file")),
		ClearAttachments: key.NewBinding(key.WithKeys("alt+d"), key.WithHelp("alt+d", "clear files")),
		ErrorDetails:     key.NewBinding(key.WithKeys("ctrl+i"), key.WithHelp("ctrl+i", "error details")),
		NewChat:          key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new chat")),
		Scroll:           key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		Jump:             key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", "top/bottom")),
		SysPrompt:        key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "system prompt")),
		Save:             key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save chat")),
		Escape:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "stop / models")),
		Quit:             key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:             key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
	}
}
