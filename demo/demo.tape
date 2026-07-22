# Records the Oolong demo GIF. Don't run vhs on this directly — use
# ./demo/record.sh, which builds the app, stands up the fake API from e2e/
# (streaming provider-specific demo replies at a realistic pace), and sets isolated fake
# credentials before invoking vhs, so the recording is deterministic.
# CI re-records it via .github/workflows/demo.yml after each tagged
# release (or manually via workflow_dispatch).

Output demo.gif

Require oolong

Set Shell "bash"
Set FontSize 32
Set Width 2100
Set Height 1500
Set Margin 20
Set MarginFill "#FFAF87"
Set BorderRadius 40

Type "oolong" Sleep 2s  Enter

Sleep 5s

# Show that the picker can collapse to a fast names-only view, then return
# to its provider-aware detail view. Adjust effort on the OpenAI model too.
Tab Sleep 2s
Tab Sleep 2s
Right@250ms 2 Sleep 2s

Down Sleep 1s Enter

Sleep 2s

Type@45ms "Show me a tiny Go timer for brewing oolong" Sleep 1s Enter

Sleep 8s

# The streamed answer contains Markdown and a fenced Go block. Copy just the
# code, then demonstrate editing the system prompt without losing the chat.
Ctrl+B Sleep 2s
Ctrl+P Sleep 1s
Type@35ms "Answer like a meticulous tea sommelier." Sleep 1s Enter
Sleep 2s

# Escape returns to the picker with the conversation intact. Move back to
# OpenAI and continue the same chat with a different native provider.
Escape Sleep 4s
Up Sleep 1s Enter
Sleep 2s
Type@45ms "Turn that into a tasting checklist" Sleep 1s Enter

Sleep 8s

# Finish on discoverability and explicit persistence: reveal every shortcut,
# hide the help again, then save the transcript on purpose.
Type "?" Sleep 4s
Type "?" Sleep 2s
