# Records the Oolong demo GIF. Don't run vhs on this directly — use
# ./demo/record.sh, which builds the app, stands up the fake API from e2e/
# (streaming demo/reply.md at a realistic pace), and sets the isolation env
# before invoking vhs, so the recording is deterministic and keyless.
# CI re-records it via .github/workflows/demo.yml after each tagged
# release (or manually via workflow_dispatch).

Output demo.gif

Require oolong

Set Shell "bash"
Set FontSize 32
Set Width 2400
Set Height 1800

Type "oolong" Sleep 2s  Enter

Sleep 7s

Down Sleep 1s Down Sleep 1s Up Sleep 1s Enter

Sleep 2s

Type "Tell me about oolong" Sleep 2s  Enter

Sleep 10s
