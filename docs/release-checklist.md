# 1.0 release checklist

Use this checklist for each release candidate and again for 1.0. Tests and
recordings must use `e2e/fakeapi`; never enter a real credential during release
verification.

## Automated gate

Run from the repository root:

```sh
test -z "$(gofmt -l .)"
go test ./...
go vet ./...
go build ./...
CGO_ENABLED=0 go build ./...
./e2e/smoke.sh
```

CI must pass on macOS, Linux, and Windows. The Linux job also proves the
no-cgo build, and the PTY smoke job covers first run, rejected key validation,
save/resume, resize, cancellation, and TUI plus one-shot requests for OpenAI,
Anthropic, Google, and Ollama.

## Manual terminal pass

Complete this short pass on macOS Terminal or iTerm2, a Linux terminal, and
Windows Terminal. Use a fake API configuration and a temporary transcript
directory.

- Launch with no keys or config; confirm setup guidance is readable and
  `ctrl+k` reaches key entry.
- Resize from a large window to roughly 72×18 and back. Confirm the picker,
  key manager, chat, composer, and centered help remain usable.
- Send a message, scroll during streaming, cancel with `esc`, then send another
  message. Confirm focus, spinner, cursor, and follow-output notice recover.
- Expand chat help, return to the picker, and confirm picker help is a single
  centered row. Confirm all three expanded-help columns include `pgup`/`pgdn`
  and `home`/`end`.
- Save and resume a transcript. Open the Markdown separately and confirm it
  contains only visible conversation text with no hidden metadata.
- Check `NO_COLOR=1`, `reduced_motion = true`, text copy, and image-paste
  availability. In a no-cgo build, image paste should be unavailable without
  affecting text copy.
- Trigger an authentication error and a connection error; confirm the concise
  recovery action and `ctrl+i` technical detail are both readable.

Record the OS, terminal, architecture, cgo setting, and result in the release
candidate notes. A platform can be signed off by CI plus this manual pass; do
not infer manual success from CI alone.

## Release candidate

1. Freeze feature work and resolve every failed gate above.
2. Review `README.md`, keybinding help, `--help`, config examples, transcript
   behavior, and provider differences against the candidate.
3. Run `./demo/record.sh` and inspect `demo/demo.gif` for clipping, stale copy,
   cursor artifacts, and real credentials.
4. Create a conventional-commit release candidate tag through the normal
   release workflow; do not hand-edit generated archives or checksums.
5. Install the produced archives on each supported OS and repeat the manual
   terminal pass before promoting 1.0.
