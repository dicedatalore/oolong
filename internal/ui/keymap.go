package ui

import "charm.land/bubbles/v2/key"

// chatKeyMap declares every key the chat screen responds to. A key.Binding
// pairs the actual key names with the label shown in the help bar; the
// bindings here drive the help display, while the key handling itself lives
// in updateChat.
type chatKeyMap struct {
	Send      key.Binding
	NewLine   key.Binding
	Paste     key.Binding
	Editor    key.Binding
	Copy      key.Binding
	CopyCode  key.Binding
	Regen     key.Binding
	Recall    key.Binding
	NewChat   key.Binding
	Scroll    key.Binding
	Jump      key.Binding
	SysPrompt key.Binding
	Save      key.Binding
	Stop      key.Binding
	Back      key.Binding
	Quit      key.Binding
	Help      key.Binding
}

// ShortHelp and FullHelp satisfy the help bubble's KeyMap interface, which
// Go picks up implicitly — no "implements" declaration needed.

// ShortHelp keeps the most used keys visible; everything else lives in the
// full help behind "?".
func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.NewLine, k.Back, k.Help}
}

func (k chatKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.NewLine, k.Paste},
		{k.Editor, k.Copy, k.CopyCode},
		{k.Regen, k.Recall, k.SysPrompt},
		{k.Save, k.NewChat, k.Scroll},
		{k.Jump, k.Stop, k.Back},
		{k.Quit, k.Help},
	}
}

func newChatKeyMap() chatKeyMap {
	return chatKeyMap{
		Send:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		NewLine:   key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line")),
		Paste:     key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste")),
		Editor:    key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "compose in $EDITOR")),
		Copy:      key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy last reply")),
		CopyCode:  key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "copy code block")),
		Regen:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "regenerate")),
		Recall:    key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "message history")),
		NewChat:   key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new chat")),
		Scroll:    key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		Jump:      key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", "top/bottom")),
		SysPrompt: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "system prompt")),
		Save:      key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save chat")),
		Stop:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "stop response")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "change model")),
		Quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
	}
}
