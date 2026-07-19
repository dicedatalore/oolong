---
name: verify
description: Build and drive the Oolong TUI end-to-end against a fake OpenAI API on a scripted pty, without touching the real API or keychain.
---

# Verifying Oolong

Oolong is a Bubble Tea TUI; drive it on a pty, never against the real API.
The committed harness in `e2e/` does the heavy lifting — reuse it instead
of building a driver from scratch:

- **`./e2e/smoke.sh`** — the full happy path (build, fake API, TUI flow,
  one-shot pipe mode) with assertions; CI runs exactly this. Run it first:
  if it passes, the harness works on this machine.
- **`e2e/drive.py`** — scripted pty driver. Answers the startup terminal
  queries (OSC 11 background, DSR, kitty, DECRQM) like a dark-background
  xterm and sets a 100x30 winsize — without those replies keystrokes get
  swallowed by the query readers and Bubble Tea sees 0x0. Env: `OOLONG_BIN`
  (binary), `OOLONG_ARGS` (extra flags, e.g. `--resume file.md`). Args:
  capture file, cwd, then `delay:keys` items — enter `\r`, esc `\x1b`,
  ctrl+s `\x13`, ctrl+f `\x06`, left `\x1b[D`, right `\x1b[C`.
- **`e2e/fakeapi`** — fake OpenAI server: `GET /v1/models` (fixed catalog),
  `POST /v1/responses` (SSE deltas + completed-with-usage). Logs request
  bodies to `$REQLOG` for asserting on params (model, reasoning, text);
  streams `$REPLY_FILE` word-by-word at `$REPLY_DELAY_MS` when set (the
  demo GIF uses this). Listens on `127.0.0.1:0` and prints
  `listening on <addr>` — parse the port from that line.

Isolation knobs (no keychain, no real API, no real config):
`OPENAI_API_KEY=sk-test`, `OPENAI_BASE_URL=http://127.0.0.1:<port>/v1`,
`XDG_CONFIG_HOME=<scratch>` (per-scenario config at
`$XDG_CONFIG_HOME/oolong/config.toml`), `OOLONG_TRANSCRIPT_DIR=<scratch>`,
`TERM=xterm-256color`.

For a custom flow, copy smoke.sh's skeleton and change the config file and
script items. Strip ANSI before asserting (smoke.sh has the regexp).

## Flows worth driving

- No config → picker shows the built-in catalog with prices; esc quits.
- Custom `[[models]]` catalog → picker holds until the availability
  check, hides models missing from `/v1/models` with a notice; dead
  API endpoint → "couldn't verify" and the whole catalog shows. (With a
  config `base_url` the check is skipped entirely — that's by design.)
- `default_model` → straight into chat, no picker frame.
- Malformed config.toml → still launches; error notice on the picker.
- Picker: left/right adjust the selected model's reasoning effort
  (shown in the item title as `effort: …` and later in the chat
  header; assert the request body carries it).
- Chat: enter opens, type + enter sends (assert the request body),
  ctrl+s saves the transcript, ctrl+f attaches a file from the cwd,
  esc back to picker, esc quits.
- One-shot needs no pty: `echo x | oolong "prompt"` asserts on stdout.
- `--resume <transcript>` restores the conversation; assert the next
  request body carries the restored history.
