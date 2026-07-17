# Oolong 🍵

**Simple ephemeral chat** — a fast, keyboard-driven terminal client for OpenAI models, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Conversations live in your terminal and nowhere else. Nothing is written to disk unless you explicitly save a transcript, and Oolong opts out of OpenAI's server-side response storage. Close the window and the chat is gone.

## Features

- **Streaming responses** rendered as markdown with syntax-highlighted code blocks
- **Ephemeral by design** — history is kept in memory only, and requests are sent with response storage disabled on OpenAI's side
- **Model picker** with per-model pricing, plus a live token count and cost estimate in the chat header
- **Image input** — paste an image from the clipboard (`ctrl+v`) and it's attached to your next message
- **System prompt editing** in place (`ctrl+p`), without losing your message draft
- **Transcript export** — `ctrl+s` saves the conversation as a timestamped markdown file
- **Keychain storage** — your API key lives in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service), not in a dotfile
- **Readable math** — LaTeX in responses is converted to plain Unicode instead of showing up as mangled backslashes

## Install

```sh
go install github.com/dicedatalore/oolong@latest
```

Requires Go 1.26+ and cgo (used for clipboard support). On Linux you'll also need the X11 development headers:

```sh
sudo apt install libx11-dev   # Debian/Ubuntu
```

Or build from source:

```sh
git clone https://github.com/dicedatalore/oolong.git
cd oolong
go build
./oolong
```

## Getting started

1. Run `oolong`.
2. On first run, paste your [OpenAI API key](https://platform.openai.com/api-keys). It's validated against the API (no tokens spent) and saved to your OS keychain. Alternatively, set `OPENAI_API_KEY` in your environment — it takes precedence over the keychain.
3. Pick a model and start chatting.

To remove a stored key: press `ctrl+k` on the model picker, or run `oolong --reset-key`.

## Keybindings

| Key | Action |
| --- | --- |
| `enter` | Send message |
| `shift+enter` / `ctrl+j` | Insert newline |
| `ctrl+v` | Paste (a clipboard image becomes an attachment) |
| `ctrl+p` | Edit the system prompt |
| `ctrl+s` | Save transcript to markdown |
| `pgup` / `pgdn` | Scroll the conversation |
| `home` / `end` | Jump to top / bottom |
| `esc` | Stop a streaming response, or go back to the model picker |
| `ctrl+c` | Stop a streaming response, or quit |
| `?` | Toggle full help |

The mouse wheel scrolls the conversation too; hold `shift` while dragging to select text, as usual in TUIs.

> **Note:** `shift+enter` requires a terminal with keyboard enhancement support (Kitty, Ghostty, WezTerm, foot, …). `ctrl+j` works everywhere.

## Privacy

- Your API key is stored in the OS keychain, never in a plain-text file.
- Chat history exists only in process memory unless you save it with `ctrl+s`.
- Requests are sent with `store: false`, so OpenAI does not retain responses for the [Responses API](https://platform.openai.com/docs/api-reference/responses)'s server-side history.

## Development

```sh
go test ./...
```

The UI is a Bubble Tea state machine with three screens — model picker, chat, and first-run key entry — each in its own file under `internal/ui`. Supporting packages: `internal/openai` (streaming client), `internal/keystore` (keychain), `internal/mathfmt` (LaTeX → Unicode), and `internal/clipboard` (image paste).

## License

[MIT](LICENSE)
