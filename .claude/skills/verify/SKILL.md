---
name: verify
description: Build and drive the Oolong TUI end-to-end against a fake OpenAI API on a scripted pty, without touching the real API or keychain.
---

# Verifying Oolong

Oolong is a Bubble Tea TUI; there is no tmux here. Drive it on a Python
pty with scripted keystrokes and capture the frames.

## Handle

- `go build -o <scratch>/oolong .` — the binary.
- Isolation knobs (no keychain, no real API, no real config):
  - `OPENAI_API_KEY=sk-test` — keystore.Resolve prefers the env var.
  - `OPENAI_BASE_URL=http://127.0.0.1:<port>/v1` — the openai-go SDK
    honors it; point it at a local fake.
  - `XDG_CONFIG_HOME=<scratch>/xdg-…` — per-scenario config files at
    `$XDG_CONFIG_HOME/oolong/config.toml`.
  - `TERM=xterm-256color`.
- Fake API: a tiny Go server for `GET /v1/models` (JSON list) and
  `POST /v1/responses` (SSE: `response.output_text.delta` events, then
  `response.completed` with usage). Log request bodies to a file to
  assert on params (model, reasoning, text). See the SSE shapes in
  `internal/openai/client_test.go`.

## The pty driver gotcha (the part that costs an hour)

The app queries the terminal at startup — OSC 11 (`ESC]11;?`, termenv
background check in main.go), DSR (`ESC[6n`), kitty (`ESC[?u`), DECRQM
(`ESC[?2026$p`, `2027`). A driver that answers nothing gets its
keystrokes swallowed by the query readers and the app never reacts.
Watch the output stream and answer each query like a dark-background
xterm (`ESC]11;rgb:1e1e/1e1e/1e1e…`, `ESC[1;1R`, `ESC[?0u`,
`ESC[?2026;0$y`); set the pty winsize via TIOCSWINSZ (e.g. 100x30) or
Bubble Tea sees 0x0. A working driver: scripted `(delay, keys)` pairs
written to the pty master, raw capture to a file, strip ANSI for
assertions (`grep -c` for notices, model names, header fragments).

## Flows worth driving

- No config → picker shows the built-in catalog with prices; esc quits.
- Custom `[[models]]` catalog → picker holds until the availability
  check, hides models missing from `/v1/models` with a notice; dead
  API endpoint → "couldn't verify" and the whole catalog shows.
- `default_model` → straight into chat, no picker frame.
- Malformed config.toml → still launches; error notice on the picker.
- Picker: left/right adjust the selected model's reasoning effort
  (shown in the item title as `effort: …` and later in the chat
  header; assert the request body carries it).
- Chat: enter opens, type + enter sends (assert the request body),
  ctrl+s saves the transcript (config `transcript_dir`;
  `OOLONG_TRANSCRIPT_DIR` wins), esc back to picker, esc quits.
  Key bytes: enter `\r`, esc `\x1b`, ctrl+c `\x03`, ctrl+s `\x13`,
  left `\x1b[D`, right `\x1b[C`.
