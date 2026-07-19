# Oolong roadmap

What's planned, roughly ordered. Releases are cut automatically from
conventional commits on main, so items ship whenever they're ready —
Now/Next/Later is priority, not versions.

## Now — quick wins [DONE]

- **`--model` flag** to open a chat directly, complementing config
  `default_model` (the `pendingModel` path in `ui.New` already does the work).
- **↑ cycles through all sent messages**, not just the last, and recalled
  messages keep their image attachments (today `lastMessage("user")` returns
  only the latest content — `internal/ui/chat.go`).
- **esc clears an applied picker filter** instead of quitting the app (today
  `updatePicker` intercepts esc whenever the filter isn't actively being
  typed).
- **Compose in `$EDITOR`** (ctrl+e): long messages round-trip through the
  user's editor via `tea.ExecProcess`.
- **Copy a code block** from the last reply, not just the whole reply
  (ctrl+y keeps its behavior).
- **`oolong config init`** scaffolds a commented config.toml at
  `config.Path()`.
- **Cap the conversation width** on very wide terminals — full-width
  paragraphs are hard to read; wrap at ~100 cols and keep the blocks
  left-aligned.

## Next — features [DONE]

- **OpenAI-compatible `base_url`** (global and per-model config) — Ollama,
  LM Studio, OpenRouter work with the existing client (`openai.New` already
  takes SDK options; the verify skill drives the app with `OPENAI_BASE_URL`
  today). First step toward multi-provider. Skip the availability check and
  key validation for non-OpenAI endpoints where they don't apply.
- **One-shot / pipe mode**: `oolong "question"` and
  `cat main.go | oolong "explain"` stream the answer to stdout with no TUI —
  reuses `internal/openai` directly. Makes Oolong scriptable.
- **Context-window meter**: per-model `context_window` (builtin catalog +
  config key), show % used in the chat header (`estimateTokens` exists),
  warn as the limit nears.
- **Resume a saved transcript**: `oolong --resume oolong-chat-….md`
  reconstructs the conversation (explicit user action only). Decision made
  at implementation time: transcripts now embed invisible HTML-comment role
  markers, and older marker-less files fall back to a fence-aware heading
  parse.
- **File attachments by path**: attach images (and text files as context)
  from disk, not just the clipboard — ctrl+f opens a file picker bubble
  over the conversation.

## Later — bigger bets

- **Native multi-provider**: extract a provider interface from
  `internal/openai` (`StreamChat`, `ListModels`, `ValidateKey` are the
  seams; `Message`/`StreamEvent` are already provider-neutral), then a
  native Anthropic client (streaming, images, usage). Per-provider keys in
  the keychain (`keystore` service name per provider), picker grouped by
  provider. `base_url` support above is the stepping stone.
- **Key manager (OpenAI + Anthropic keys)** — phase 1 of native
  multi-provider. `keystore` gains a `Provider` type with per-provider
  keychain accounts (`openai_api_key` stays as-is, so no migration) plus a
  `Source()` for the env/keychain/not-set display. Picker ctrl+k opens a
  two-row manager screen (add/edit/remove, masked key tails, env rows
  read-only) instead of today's delete-on-the-spot. Key entry becomes
  provider-aware; Anthropic validation is a bare `GET /v1/models` in a new
  `internal/anthropic` package the native client later grows into. An
  Anthropic key is inert until that client lands — ship anyway for the
  non-destructive ctrl+k and the keystore foundation. First-run stays
  OpenAI-only key entry (zero-config decision). `keyring.MockInit()` keeps
  tests off the real keychain.

## Infra & distribution

- **E2E pty smoke test in CI** [DONE]: `e2e/smoke.sh` drives the happy path
  (picker → send → save → quit, plus one-shot pipe mode) on a pty against
  `e2e/fakeapi`; the `e2e` job in ci.yml runs it on every push/PR.
- **GIF update pipeline** [DONE]: `demo/record.sh` records `demo/demo.tape`
  via Charm VHS against the fake API (canned `demo/reply.md`, no key);
  `.github/workflows/demo.yml` re-records on tape changes or manual dispatch
  and auto-commits the GIF (`chore:` — never cuts a release).
- **macOS signing + notarization** — removes the cask's quarantine-stripping
  hook. Needs an Apple Developer ID cert + notary API key as repo secrets;
  goreleaser's quill-based `notarize.macos` is the intended mechanism.

## Standing decisions

- **Ephemeral by default**: nothing is written or read without an explicit
  user action; resume only ever loads a file the user chose to save.
- **Multi-provider is the direction** (supersedes "OpenAI only"): compatible
  endpoints first, native providers after — never at the cost of the
  zero-config experience.
- **Keyboard-first, one screen at a time**: features that add chrome must
  justify themselves.

## Not for now, [DON'T DO]

- **Reasoning summaries**: stream the Responses API reasoning-summary deltas
  as dim "thinking…" text above the reply (`internal/ui/stream.go` only
  handles `response.output_text.delta` today) — makes high-effort models
  feel alive during long pauses.
- **Session budget**: `budget_usd` config key; warn when the running
  estimate crosses it.
- **In-chat search** through the transcript.
- **Inline image preview** for pasted attachments via the kitty/iTerm2
  graphics protocols, where supported.
- **More channels**: Homebrew formula (Linux), winget.
