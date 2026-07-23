# Oolong 🍵

[![CI](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml/badge.svg)](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml)

**A fast, ephemeral chat client for your terminal.**

Oolong brings OpenAI, Anthropic, Google, Ollama, and compatible endpoints into
one compact, keyboard-first interface. Change models without abandoning the
conversation, attach files and images, or send a prompt through a shell
pipeline. Chats remain in memory unless you explicitly save them.

Make it yours without rebuilding it. Choose the model catalog and default
model, connect local or hosted endpoints, set the accent colors, simplify the
model picker, reduce motion, and tailor the system prompt from inside the TUI.
Configuration is optional, and an invalid optional setting will never prevent
Oolong from starting.

![Oolong terminal chat demo](./demo/demo.gif)

## Why Oolong?

- **Move freely between models** — switch providers mid-conversation and keep
  the context.
- **Stay private by default** — chats are held in memory and saved only when
  you ask.
- **Work at terminal speed** — send, edit, retry, attach, inspect, and copy
  without reaching for a mouse.
- **Use the interface that fits** — open the TUI for a conversation or stream
  a one-shot answer to stdout.
- **Shape the experience** — customize models, endpoints, colors, motion, and
  picker density with a small TOML file.

## Install

### macOS

```sh
brew install --cask dicedatalore/tap/oolong
oolong
```

### Windows

```powershell
scoop bucket add dicedatalore https://github.com/dicedatalore/scoop-bucket.git
scoop install dicedatalore/oolong
oolong
```

### Go

On macOS, Linux, or Windows with Go 1.26 or later:

```sh
go install github.com/dicedatalore/oolong@latest
oolong
```

Prebuilt archives for macOS, Linux, and Windows are also available from the
[releases page](https://github.com/dicedatalore/oolong/releases).

Press `ctrl+k` to add an OpenAI, Anthropic, or Google API key, then choose a
model. Oolong stores saved keys in the operating system keychain and also
recognizes provider environment variables. Ollama and local custom endpoints
do not require a key.

## Use Oolong

Run `oolong` with no arguments to open the TUI. Pass a prompt or pipe input to
use one-shot mode:

```sh
oolong "explain how DNS caching works"
git diff | oolong "write a concise commit message"
oolong --model MODEL "summarize this directory structure"
```

Arguments and piped input use the same model catalog, credentials, and provider
settings as the TUI.

## Customize Oolong

Run `oolong config init` to create a documented starter configuration, then
uncomment only the settings you want to change:

```toml
default_model = "gpt-7.2-mercury"
accent = "#FFAF87"
secondary_accent = "#7D56F4"
simple_picker = true
reduced_motion = true

[[models]]
id = "gpt-7.2-mercury"
provider = "openai"
description = "Balances intelligence and cost"
input_rate = 2.50
output_rate = 15.00
reasoning_effort = "medium"
context_window = 400000
```

Adding one or more `[[models]]` entries replaces the built-in catalog. Every
custom model requires an `id` and `provider`; descriptions, rates, reasoning
effort, verbosity, context window, and endpoint are optional. If an optional
configuration file is malformed or unavailable, Oolong starts with safe
defaults and reports the issue in the interface.

You can also press `ctrl+p` during a chat to edit its system prompt. Press
`tab` in the model picker to move between detailed and compact views.

### Providers

| Provider | API | Credential |
| --- | --- | --- |
| OpenAI | Responses | `OPENAI_API_KEY` or keychain |
| Anthropic | Messages | `ANTHROPIC_API_KEY` or keychain |
| Google | Gemini | `GEMINI_API_KEY` or keychain |
| Ollama | Native `/api/chat` | None |

Image input, reasoning effort, and context limits vary by model. After a
provider error, press `ctrl+i` to view the original technical detail.

### Custom OpenAI endpoints

Set `base_url` on an OpenAI model to use a service that implements the
Responses API, such as LM Studio or OpenRouter:

```toml
[[models]]
id = "local-model"
provider = "openai"
base_url = "http://localhost:1234/v1"
```

Local endpoints require no key. `OPENAI_BASE_URL` overrides configured OpenAI
endpoints. To use Ollama's native API, set `provider = "ollama"` instead.

## Save and resume chats

Press `ctrl+s` to save the current chat as readable Markdown. Resume it later
with:

```sh
oolong --resume oolong-chat.md
```

Oolong loads a transcript only when you explicitly pass `--resume`.
Attachments are recorded by name; their contents are not embedded in the
transcript.

## Keybindings

| Key | Action |
| --- | --- |
| `enter` | Send message |
| `shift+enter` / `ctrl+j` | Insert newline |
| `ctrl+v` | Paste; a clipboard image becomes an attachment |
| `ctrl+f` | Attach an image or text file from disk |
| `ctrl+y` | Copy the latest reply |
| `ctrl+b` | Copy the last code block in the latest reply |
| `ctrl+r` | Regenerate the latest reply |
| `ctrl+u` | Edit the latest user message and regenerate from it |
| `ctrl+t` | Retry the latest request with another model |
| `ctrl+k` | Open the provider key manager from an error |
| `ctrl+i` | Show or hide technical error details |
| `↑` / `↓` | Cycle through sent messages and their attachments when the composer is empty |
| `ctrl+d` / `alt+d` | Remove the latest pending attachment / clear all pending attachments |
| `ctrl+n` | Start a new chat |
| `ctrl+p` | Edit the system prompt |
| `ctrl+s` | Save the transcript as Markdown |
| `pgup` / `pgdn` | Scroll the conversation |
| `home` / `end` | Jump to the top / bottom |
| `esc` | Stop a response, or return to model selection without losing the conversation |
| `ctrl+c` | Quit |
| `?` | Toggle full help |

In the model picker, `←` and `→` change reasoning effort, while `tab` toggles
the compact view. Use `ctrl+j` for a newline if your terminal does not support
`shift+enter`.

## CLI reference

```text
oolong                       Open the chat TUI
oolong "PROMPT"              Stream a one-shot answer to stdout
... | oolong ["PROMPT"]      Send piped input as one-shot context
oolong --model MODEL         Skip the model picker
oolong --resume FILE         Resume a saved transcript in the TUI
oolong config init           Create a starter configuration
oolong doctor                Inspect the local setup without provider traffic
oolong --reset-key           Delete stored API keys
oolong --version             Print the installed version
```

Use `--provider` with `--model` when the model is not present in the catalog.
Set `NO_COLOR=1` to disable color.

## Privacy and credentials

- Chats, attachments, and prompts remain in memory unless you explicitly save
  a transcript.
- Saved API keys are stored in the operating system keychain, not in Oolong's
  configuration file.
- OpenAI requests set `store: false`.
- `oolong doctor` inspects local configuration without contacting a provider.

## Build from source

```sh
go test ./...
go vet ./...
go build ./...
```

Clipboard image paste requires cgo. Linux cgo builds also require X11
development headers. Oolong still builds with `CGO_ENABLED=0`; only clipboard
image paste is unavailable, while text copy continues to work.

The TUI lives in `internal/ui`, and provider clients live in
`internal/provider`. Conventional commit subjects control automated version
bumps: `feat:` releases a minor version, `fix:` releases a patch, and breaking
changes release a major version.

## License

[MIT](LICENSE)
