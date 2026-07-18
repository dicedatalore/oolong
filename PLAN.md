# Oolong roadmap

What's planned, roughly ordered. Releases are cut automatically from
conventional commits on main, so items ship whenever they're ready —
Now/Next/Later is priority, not versions.

## Now — quick wins

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

## Next — features

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
  reconstructs the conversation (explicit user action only). Decision at
  implementation time: parse the markdown back, or have ctrl+s also write a
  structured sidecar.
- **Prompt presets**: named system prompts in config (`[[prompts]]`), picked
  from the ctrl+p flow.
- **File attachments by path**: attach images (and text files as context)
  from disk, not just the clipboard.
- **Reasoning summaries**: stream the Responses API reasoning-summary deltas
  as dim "thinking…" text above the reply (`internal/ui/stream.go` only
  handles `response.output_text.delta` today) — makes high-effort models
  feel alive during long pauses.
- **Session budget**: `budget_usd` config key; warn when the running
  estimate crosses it.

## Later — bigger bets

- **Native multi-provider**: extract a provider interface from
  `internal/openai` (`StreamChat`, `ListModels`, `ValidateKey` are the
  seams; `Message`/`StreamEvent` are already provider-neutral), then a
  native Anthropic client (streaming, images, usage). Per-provider keys in
  the keychain (`keystore` service name per provider), picker grouped by
  provider. `base_url` support above is the stepping stone.
- **In-chat search** through the transcript.
- **Inline image preview** for pasted attachments via the kitty/iTerm2
  graphics protocols, where supported.
- **Multiple conversations (tabs)** — only if it can stay simple; one
  screen at a time is part of the product.

## Infra & distribution

- **E2E pty smoke test in CI**: the verify skill already documents the whole
  driver (OSC/DSR replies, winsize, fake `/v1/responses` SSE server);
  automate one happy-path flow.
- **macOS signing + notarization** — removes the cask's quarantine-stripping
  hook.
- **More channels**: Homebrew formula (Linux), winget, scoop, AUR, a Nix
  flake — pick by demand.

## Standing decisions

- **Ephemeral by default**: nothing is written or read without an explicit
  user action; resume only ever loads a file the user chose to save.
- **Multi-provider is the direction** (supersedes "OpenAI only"): compatible
  endpoints first, native providers after — never at the cost of the
  zero-config OpenAI experience.
- **Keyboard-first, one screen at a time**: features that add chrome must
  justify themselves.
