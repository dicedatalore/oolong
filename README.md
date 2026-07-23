# Oolong 🍵

[![CI](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml/badge.svg)](https://github.com/dicedatalore/oolong/actions/workflows/ci.yml)

**Simple, ephemeral, chat.**

Oolong is a fast, keyboard-first chat client for OpenAI, Anthropic, Google,
Ollama, and custom OpenAI endpoints. Switch models halfway through a
conversation, attach files and images, or skip the TUI and use it in a pipe.
Chats stay in memory unless you explicitly save them.

![oolong demo](./demo/demo.gif)

## Try it

On macOS:

```sh
brew install --cask dicedatalore/tap/oolong
oolong
```

On Windows with Scoop:

```powershell
scoop bucket add dicedatalore https://github.com/dicedatalore/scoop-bucket.git
scoop install dicedatalore/oolong
oolong
```

Or install with Go on macOS, Linux, or Windows:

```sh
go install github.com/dicedatalore/oolong@latest
oolong
```

Press `ctrl+k` to add an OpenAI, Anthropic, or Google key, then choose a model.
Oolong stores keys in the OS keychain; environment variables work too. Ollama
and local custom endpoints need no key.

Already have a shell task in mind? Arguments and piped input automatically use
one-shot mode:

```sh
oolong "explain how DNS caching works"
git diff | oolong "write a concise commit message"
```

## Why Oolong?

- **One conversation, any model** — switch providers without starting over.
- **Private by default** — chats stay in memory, and are only saved when you ask them to be.
- **Built for the keyboard** — edit, retry, inspect usage, and copy without reaching for a mouse.
- **TUI when you want it, stdout when you don't** — use the same models interactively or in shell pipelines.

## Other installation options

Prebuilt archives for macOS, Linux, and Windows are on the
[releases page](https://github.com/dicedatalore/oolong/releases).

Building with Go requires Go 1.26+. Clipboard image paste also needs cgo and,
on Linux, X11 development headers. Without cgo the build still works; only
image paste is disabled.

## Configuration

Configuration is optional. Run `oolong config init` to create a commented
config file, then change only what you need:

```toml
default_model = "gpt-7.2-mercury"
accent = "#FFAF87"
simple_picker = true

[[models]]
id = "gpt-7.2-mercury"
provider = "openai"
description = "Balances intelligence and cost"
input_rate = 2.50
output_rate = 15.00
reasoning_effort = "medium"
context_window = 400000
```

Adding any `[[models]]` entries replaces the built-in catalog. Each model needs
an `id` and `provider`; all other fields are optional. Invalid configuration is
ignored at startup rather than blocking Oolong.

### Providers

| Provider | API | Credential |
| --- | --- | --- |
| OpenAI | Responses | `OPENAI_API_KEY` or keychain |
| Anthropic | Messages | `ANTHROPIC_API_KEY` or keychain |
| Google | Gemini | `GEMINI_API_KEY` or keychain |
| Ollama | Native `/api/chat` | None |

Support for images, effort, and large inputs varies by model. Press `ctrl+i`
after an error to see the provider's original detail.

### Custom OpenAI endpoints

Use the OpenAI provider with a `base_url` for services that implement the
Responses API, such as LM Studio or OpenRouter:

```toml
[[models]]
id = "local-model"
provider = "openai"
base_url = "http://localhost:1234/v1"
```

Local endpoints need no key. `OPENAI_BASE_URL` overrides configured OpenAI
endpoints. For Ollama, use `provider = "ollama"` instead.

## CLI shortcuts

```sh
oolong --model MODEL                        # skip the picker
oolong "why is the sky blue"                # stream to stdout
cat main.go | oolong "explain this file"    # pipe in a file
```

Arguments and stdin use the same provider settings as the TUI.

## Saving chats

Press `ctrl+s` to save a chat as readable Markdown, then resume it later:

```sh
oolong --resume oolong-chat.md
```

Attachments are represented by their names; their contents are not embedded.
Oolong only loads a transcript when you explicitly pass `--resume`.

## Keybindings

| Key | Action |
| --- | --- |
| `enter` | Send message |
| `shift+enter` / `ctrl+j` | Insert newline |
| `ctrl+v` | Paste (a clipboard image becomes an attachment) |
| `ctrl+f` | Attach an image or text file from disk |
| `ctrl+y` | Copy the last reply to the clipboard |
| `ctrl+b` | Copy the last reply's last code block |
| `ctrl+r` | Regenerate the last reply |
| `ctrl+u` | Edit the latest user message and regenerate from it |
| `ctrl+t` | Retry the last request with another model |
| `ctrl+k` | Open the provider key manager from an error |
| `ctrl+i` | Show or hide technical error details |
| `↑` / `↓` | Cycle through your sent messages, attachments included (when the composer is empty) |
| `ctrl+d` / `alt+d` | Remove the last pending attachment / clear all pending attachments |
| `ctrl+n` | Start a new chat |
| `ctrl+p` | Edit the system prompt |
| `ctrl+s` | Save transcript to markdown |
| `pgup` / `pgdn` | Scroll the conversation |
| `home` / `end` | Jump to top / bottom |
| `esc` | Stop a streaming response, or switch model (the conversation is kept) |
| `ctrl+c` | Quit |
| `?` | Toggle full help |

In the picker, `←`/`→` changes reasoning effort and `tab` toggles the compact
view. Use `ctrl+j` for a newline if your terminal does not support
`shift+enter`.

## Terminal options

- Set `NO_COLOR=1` to disable color.

## Privacy

- Keys are stored in the OS keychain, not a config file.
- Chats stay in memory unless you press `ctrl+s`.
- OpenAI requests set `store: false`.

## Development

```sh
go test ./...
go vet ./...
go build ./...
```

The TUI lives in `internal/ui`; provider clients live in `internal/provider`.
Conventional commits control version bumps: `feat:` is minor, `fix:` is patch,
and breaking changes are major.

## License

[MIT](LICENSE)
