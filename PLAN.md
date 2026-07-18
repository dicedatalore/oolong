# Oolong v0.3 — configurability

The v0.2 cycle invested in the in-session experience (model switch, regenerate,
copy, recall, live cost). v0.3 makes Oolong configurable without touching the
source, relieving the main pressure point: the hardcoded model catalog.

## 1. Config file

`~/.config/oolong/config.toml` (respect `$XDG_CONFIG_HOME`), all keys optional —
zero-config behavior stays exactly as today.

```toml
default_model = "gpt-5.6-terra"   # skip the picker on launch when set
transcript_dir = "~/notes/chats"  # OOLONG_TRANSCRIPT_DIR env var still wins
accent = "#FFAF87"                # primary accent color

# Replaces the built-in catalog when present — the pressure release valve
# for model launches and price changes between Oolong releases.
[[models]]
id = "gpt-5.6-terra"
description = "Balances intelligence and cost"
input_rate = 2.50   # USD per 1M tokens
output_rate = 15.00
```

Should be able to add any available openai model to the catalog eg GPT-5.4. must check if available before displaying in the picker.

Implementation notes:

- New `internal/config` package; load in `main.go`, pass into `ui.New`.
- The `rates` map and picker items in `internal/ui/picker.go:26` become derived
  from config, falling back to the built-in list.
- `transcript_dir` merges with the existing env-var handling in
  `internal/ui/transcript.go` (env var overrides config).
- Malformed config: show the error and continue with defaults — never block
  launch on a bad config file.

## 2. Reasoning / verbosity controls

- Expose the Responses API `reasoning.effort` (and text verbosity) parameters
  for models that support them: per-model config key plus a session override.
- Plumbing: `internal/openai.StreamChat` gains options; the chat header shows
  the active effort level.
- Costs: reasoning tokens are billed as output — the existing usage handling
  in `internal/ui/stream.go` already counts them; verify the live estimate
  stays sane for long thinking pauses (no deltas while reasoning).

## Out of scope (standing decisions)

- Saved sessions / history — Oolong stays strictly ephemeral.
- Multi-provider support — OpenAI only.

## Verification

- `go test ./...`; new config package gets table-driven parse/merge tests.
- Manual: launch with no config, a partial config, a malformed config; confirm
  the picker reflects a custom catalog and `default_model` skips the picker.
