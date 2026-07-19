# Oolong 🍵

[![CI](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml/badge.svg)](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml)

**Simple ephemeral chat** — a fast, keyboard-driven terminal client for OpenAI models, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

- **Ephemeral by design** — conversations live in your terminal and nowhere else. Nothing is written to disk unless you save a transcript, and OpenAI's server-side response storage is switched off. Close the window and the chat is gone.
- **Any OpenAI-compatible endpoint** — the official API works out of the box, or point `base_url` at Ollama, LM Studio, or OpenRouter and give local models the same polished UI.
- **Scriptable** — `git diff | oolong "write a commit message"` streams the answer straight to stdout, no TUI, so Oolong drops into any shell pipeline.

![oolong demo](./demo/demo.gif)

## Features

- **Streaming responses** rendered as markdown with syntax-highlighted code blocks
- **Ephemeral by design** — history is kept in memory only, and requests are sent with response storage disabled on OpenAI's side
- **Model picker** with per-model pricing, plus a live token count and cost estimate in the chat header
- **Mid-chat model switch** — `esc` back to the picker keeps the conversation, so you can escalate to a bigger model halfway through
- **Image input** — paste an image from the clipboard (`ctrl+v`) and it's attached to your next message
- **File attachments** — `ctrl+f` picks an image or text file from disk to send with your next message
- **One-shot mode** — `oolong "question"` (or `cat main.go | oolong "explain"`) streams the answer to stdout with no TUI, so Oolong works in scripts and pipelines
- **OpenAI-compatible endpoints** — point `base_url` at Ollama, LM Studio, OpenRouter, or any compatible server, globally or per model
- **Context meter** — the chat header tracks how much of the model's context window the conversation fills, and warns as it nears the limit
- **System prompt editing** in place (`ctrl+p`), without losing your message draft
- **Transcript export & resume** — `ctrl+s` saves the conversation as a timestamped markdown file; `oolong --resume <file>` picks it back up later
- **Configurable** — an optional TOML config file sets a custom model catalog, a default model, reasoning effort and verbosity, endpoints, transcript directory, and accent color
- **Keychain storage** — your API key lives in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service), not in a dotfile
- **Readable math** — LaTeX in responses is converted to plain Unicode instead of showing up as mangled backslashes

## Install

### Homebrew (macOS)

```sh
brew install --cask dicedatalore/tap/oolong
```

### With Go

```sh
go install github.com/dicedatalore/oolong@latest
```

Works on macOS, Linux, and Windows with Go 1.26+. Clipboard image paste needs cgo — a C compiler, plus the X11 development headers on Linux:

```sh
sudo apt install libx11-dev   # Debian/Ubuntu
```

Without cgo the build still works; image paste is simply disabled.

### From source

```sh
git clone https://github.com/dicedatalore/oolong.git
cd oolong
go build
./oolong
```

Prefer a standalone binary? Prebuilt archives for macOS, Linux, and Windows are on the [releases page](https://github.com/dicedatalore/oolong/releases). On Windows, run Oolong from [Windows Terminal](https://aka.ms/terminal) — the legacy console doesn't render TUIs well.

## Getting started

1. Run `oolong`.
2. On first run, paste your [OpenAI API key](https://platform.openai.com/api-keys). It's validated against the API (no tokens spent) and saved to your OS keychain. Alternatively, set `OPENAI_API_KEY` in your environment — it takes precedence over the keychain.
3. Pick a model and start chatting.

To remove a stored key: press `ctrl+k` on the model picker, or run `oolong --reset-key`.

## Configuration

Oolong is fully usable with no configuration. To customize it, run `oolong config init` to scaffold a commented `~/.config/oolong/config.toml` (`$XDG_CONFIG_HOME` is respected); every key is optional:

```toml
default_model = "gpt-5.6-terra"   # skip the picker on launch
transcript_dir = "~/notes/chats"  # OOLONG_TRANSCRIPT_DIR still wins
accent = "#FFAF87"                # primary accent color
# base_url = "https://api.openai.com/v1"
# provider = "openai"

# Replaces the built-in model catalog when present. Any model your API key
# can access works — entries are checked against the API and unavailable
# ones are hidden from the picker.
[[models]]
id = "gpt-5.4"
provider = "openai"
description = "Previous generation"
input_rate = 1.25    # USD per 1M tokens, both optional
output_rate = 10.00
reasoning_effort = "medium"  # gpt-5.6 takes none | low | medium | high | xhigh
verbosity = "low"            # low | medium | high
context_window = 400000      # tokens; shows a ctx meter in the chat header

[[models]]
id = "gemma3"
provider = "ollama"
description = "Local Gemma through Ollama"
base_url = "http://localhost:11434"
```

For a single run, `oolong --model <id>` opens a chat directly with any model your key can access, overriding `default_model`.

`reasoning_effort` and `verbosity` set the model's default [Responses API](https://platform.openai.com/docs/api-reference/responses) parameters. They're passed through as-is — the supported values vary by model generation, and the API reports clearly if a model rejects one. On the model picker, `←`/`→` adjust the selected model's effort for the session, shown in the list item and later in the chat header. A malformed config never blocks launch — Oolong falls back to defaults and shows what it ignored.

### OpenAI-compatible endpoints

`base_url` points Oolong at any server that speaks the OpenAI API — Ollama, LM Studio, OpenRouter, and friends. Set it globally, or per model to mix endpoints in one catalog. Local endpoints need no API key; on custom endpoints Oolong skips the OpenAI-specific key validation and model availability check. The `OPENAI_BASE_URL` environment variable overrides every configured endpoint.

Set `provider = "openai"` for OpenAI and compatible endpoints, or `provider = "ollama"` for Ollama's native API. Provider selection can be global or per model, so one catalog can contain both. Ollama remains opt-in; an empty config still uses Oolong's built-in OpenAI catalog. Both `http://localhost:11434` and its `/v1` form are accepted for Ollama.

## Scripting

Positional arguments (or piped input) skip the TUI entirely and stream the answer to stdout:

```sh
oolong "why is the sky blue"
cat main.go | oolong "explain this file"
git diff | oolong "write a commit message"
```

One-shot mode uses `--model` / `default_model` (falling back to the catalog's first entry) and the same key, endpoint, and reasoning settings as the TUI.

## Resuming a chat

Transcripts saved with `ctrl+s` can be picked back up later:

```sh
oolong --resume oolong-chat-2026-07-19-094035.md
```

The conversation, system prompt, and model are restored from the file (image and file attachments are recorded only as labels, so they don't ride along). Nothing is ever loaded implicitly — resume only reads a file you name.

## Keybindings

| Key | Action |
| --- | --- |
| `enter` | Send message |
| `shift+enter` / `ctrl+j` | Insert newline |
| `ctrl+v` | Paste (a clipboard image becomes an attachment) |
| `ctrl+f` | Attach an image or text file from disk |
| `ctrl+e` | Compose the message in `$EDITOR` |
| `ctrl+y` | Copy the last reply to the clipboard |
| `ctrl+b` | Copy the last reply's last code block |
| `ctrl+r` | Regenerate the last reply |
| `↑` / `↓` | Cycle through your sent messages, attachments included (when the composer is empty) |
| `ctrl+n` | Start a new chat |
| `ctrl+p` | Edit the system prompt |
| `ctrl+s` | Save transcript to markdown |
| `pgup` / `pgdn` | Scroll the conversation |
| `home` / `end` | Jump to top / bottom |
| `esc` | Stop a streaming response, or switch model (the conversation is kept) |
| `ctrl+c` | Quit |
| `?` | Toggle full help |

On the model picker, `←`/`→` adjust the selected model's reasoning effort before the chat starts, and `esc` clears an active filter before it quits.

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

The UI is a Bubble Tea state machine with three screens — model picker, chat, and first-run key entry — each in its own file under `internal/ui`. Supporting packages: `internal/openai` (streaming client), `internal/oneshot` (pipe mode), `internal/keystore` (keychain), `internal/mathfmt` (LaTeX → Unicode), and `internal/clipboard` (image paste).

Releases are cut automatically on push to `main`: the version bump is derived from [conventional commit](https://www.conventionalcommits.org) messages — `feat:` → minor, `fix:` → patch, a breaking change → major — and commits of other types (`chore:`, `docs:`, `test:`, …) don't trigger a release.

## License

[MIT](LICENSE)
