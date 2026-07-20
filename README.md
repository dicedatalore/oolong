# Oolong 🍵

[![CI](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml/badge.svg)](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml)

**Simple ephemeral chat** — a fast, keyboard-driven terminal client for OpenAI, Anthropic, Google, and Ollama models, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

- **Ephemeral by design** — conversations live in your terminal and nowhere else. Nothing is written to disk unless you save a transcript, and OpenAI's server-side response storage is switched off. Close the window and the chat is gone.
- **Four native clients** — use OpenAI's Responses API, Anthropic's Messages API, Google's Gemini API, or Ollama's local chat API from the same interface. Services that implement OpenAI's Responses API, such as LM Studio and OpenRouter, are supported too.
- **Scriptable** — `git diff | oolong "write a commit message"` streams the answer straight to stdout, no TUI, so Oolong drops into any shell pipeline.

![oolong demo](./demo/demo.gif)

## Features

- **Streaming responses** rendered as markdown with syntax-highlighted code blocks
- **Ephemeral by design** — history is kept in memory only, and requests are sent with response storage disabled on OpenAI's side
- **Model picker** grouped by provider with per-model pricing (`tab` switches to a simple names-only view), plus a live token count and cost estimate in the chat header
- **Mid-chat model switch** — `esc` back to the picker keeps the conversation, so you can escalate to a bigger model halfway through
- **Image input** — paste an image from the clipboard (`ctrl+v`) and it's attached to your next message
- **File attachments** — `ctrl+f` picks an image or text file from disk to send with your next message
- **One-shot mode** — `oolong "question"` (or `cat main.go | oolong "explain"`) streams the answer to stdout with no TUI, so Oolong works in scripts and pipelines
- **Native provider support** — OpenAI, Anthropic, Google, and Ollama clients stream text, images, files, system prompts, effort, and usage through their respective APIs
- **Custom OpenAI endpoints** — use the OpenAI client with a custom `base_url` for services such as LM Studio and OpenRouter, globally or per model
- **Context meter** — the chat header tracks how much of the model's context window the conversation fills, and warns as it nears the limit
- **System prompt editing** in place (`ctrl+p`), without losing your message draft
- **Transcript export & resume** — `ctrl+s` saves the conversation as a timestamped markdown file; `oolong --resume <file>` picks it back up later
- **Configurable** — an optional TOML config file sets a custom model catalog, a default model, reasoning effort and verbosity, endpoints, transcript directory, and accent color
- **Keychain storage** — provider API keys live in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service), not in a dotfile
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

Without cgo the build still works; image paste is disabled, while text copy
continues to work in terminals that support OSC52.

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
2. Press `ctrl+k` to open the key manager. It accepts OpenAI, Anthropic, and Google keys and stores them only in your OS keychain. `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, and `GEMINI_API_KEY` take precedence when set.
3. Pick a model and start chatting.

To add, replace, or remove a provider key, press `ctrl+k` on the model picker. `oolong --reset-key` removes all stored provider keys.

## Configuration

Oolong is fully usable with no configuration. To customize it, run `oolong config init` to scaffold a commented `~/.config/oolong/config.toml` (`$XDG_CONFIG_HOME` is respected); every key is optional.

The catalog below demonstrates all four clients in one configuration. Adding any `[[models]]` entries replaces the built-in catalog, so include every model you want to appear in the picker:

```toml
default_model = "gpt-5.6-terra"   # skip the picker on launch
transcript_dir = "~/notes/chats"  # OOLONG_TRANSCRIPT_DIR still wins
accent = "#FFAF87"                # primary accent color
simple_picker = true              # start the picker in its simple view (tab toggles)

# OpenAI client — uses the official Responses API when base_url is omitted.
[[models]]
id = "gpt-5.6-terra"
provider = "openai"
description = "Balances intelligence and cost"
input_rate = 2.50             # USD per 1M tokens; rates are optional
output_rate = 15.00
reasoning_effort = "medium"   # model-dependent
verbosity = "low"             # OpenAI only; model-dependent
context_window = 400000       # enables the context meter

# Anthropic client — uses the native Messages API.
[[models]]
id = "claude-sonnet-5"
provider = "anthropic"
description = "Balanced speed, cost, and intelligence"
input_rate = 2.00
output_rate = 10.00
reasoning_effort = "medium"
context_window = 1000000

# Google client — uses the native Gemini API.
[[models]]
id = "gemini-3.5-flash"
provider = "google"
description = "Fast, capable everyday model"
input_rate = 1.50
output_rate = 9.00
reasoning_effort = "medium"
context_window = 1000000

# Ollama client — uses the native /api/chat endpoint and needs no API key.
[[models]]
id = "gemma3"
provider = "ollama"
description = "Local Gemma through Ollama"
base_url = "http://localhost:11434"
context_window = 128000
```

For a single run, `oolong --model <id>` opens a chat directly with a configured model, overriding `default_model`.

`provider` selects the client and may be `openai`, `anthropic`, `google`, or `ollama`. It can be set globally or per model; per-model values let one catalog mix providers. A global `base_url` is inherited by models using the global provider, while a per-model value overrides it. Models selecting another provider use that provider's official endpoint unless they set their own `base_url`.

`reasoning_effort` sets the provider's effort parameter; `verbosity` applies only to OpenAI's Responses API. Values are passed through because support varies by model generation. On the model picker, `←`/`→` adjust effort for the session. A malformed config never blocks launch — Oolong falls back to defaults and shows what it ignored.

### Custom OpenAI endpoints

Use `provider = "openai"` with a custom `base_url` for a service that implements OpenAI's Responses API, such as LM Studio or OpenRouter:

```toml
[[models]]
id = "local-model"
provider = "openai"
base_url = "http://localhost:1234/v1"
description = "Model served by LM Studio"
```

Local custom endpoints need no API key, and Oolong skips OpenAI-specific key and model validation for them. `OPENAI_BASE_URL` overrides configured OpenAI endpoints only. For Ollama, prefer `provider = "ollama"`; both `http://localhost:11434` and its `/v1` form are accepted and normalized to the native API.

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

The conversation, system prompt, model, and attachments are restored from a
versioned metadata block in the Markdown file. Nothing is ever loaded
implicitly — resume only reads a file you name. Because saved transcripts are
lossless, their metadata contains the contents of attached files and images;
treat transcript files as private data.

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

On the model picker, `←`/`→` adjust the selected model's reasoning effort before the chat starts, `tab` toggles between the full view (descriptions, rates, provider headings) and a simple one-line-per-model view, and `esc` clears an active filter before it quits. Set `simple_picker = true` in the config to start in the simple view.

The mouse wheel scrolls the conversation too; hold `shift` while dragging to select text, as usual in TUIs.

> **Note:** `shift+enter` requires a terminal with keyboard enhancement support (Kitty, Ghostty, WezTerm, foot, …). `ctrl+j` works everywhere.

## Privacy

- Provider API keys are stored in the OS keychain, never in a plain-text file.
- Chat history exists only in process memory unless you save it with `ctrl+s`.
- Requests are sent with `store: false`, so OpenAI does not retain responses for the [Responses API](https://platform.openai.com/docs/api-reference/responses)'s server-side history.

## Development

```sh
go test ./...
```

The UI is a Bubble Tea state machine with three screens — model picker, chat, and provider key manager — under `internal/ui`. Provider clients live in `internal/openai`, `internal/anthropic`, `internal/google`, and `internal/ollama`; `internal/provider` is the shared route resolver and client factory used by both TUI and one-shot modes. Supporting packages handle configuration, keychain access, math formatting, and clipboard image integration.

Releases are cut automatically on push to `main`: the version bump is derived from [conventional commit](https://www.conventionalcommits.org) messages — `feat:` → minor, `fix:` → patch, a breaking change → major — and commits of other types (`chore:`, `docs:`, `test:`, …) don't trigger a release.

## License

[MIT](LICENSE)
