#!/usr/bin/env bash
# End-to-end smoke test: build Oolong, stand up the fake OpenAI API, drive
# one happy-path TUI flow on a pty, then check one-shot pipe mode. Fails
# loudly (and dumps the captured frames) on any missed assertion.
#
# Everything is isolated: env API key, fake endpoint, empty XDG config, and
# a temp transcript dir — no keychain, no real network, no user config.
set -euo pipefail
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
cleanup() {
    if [ -n "${FAKEAPI_PID:-}" ]; then
        kill "$FAKEAPI_PID" 2>/dev/null || true
        wait "$FAKEAPI_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT

echo "== build"
go build -o "$TMP/oolong" .
go build -o "$TMP/fakeapi" ./e2e/fakeapi

echo "== start fake API"
REQLOG="$TMP/reqlog.txt" "$TMP/fakeapi" 127.0.0.1:0 > "$TMP/fakeapi.out" &
FAKEAPI_PID=$!
for _ in $(seq 50); do
    ADDR=$(sed -n 's/^listening on //p' "$TMP/fakeapi.out")
    [ -n "$ADDR" ] && break
    sleep 0.1
done
[ -n "$ADDR" ] || { echo "fakeapi never came up"; exit 1; }
echo "   fakeapi on $ADDR"

export OPENAI_API_KEY=sk-test
export OPENAI_BASE_URL="http://$ADDR/v1"
export XDG_CONFIG_HOME="$TMP/xdg"
export OOLONG_TRANSCRIPT_DIR="$TMP/transcripts"
export TERM=xterm-256color

strip_ansi() {
    python3 -c '
import re, sys
raw = open(sys.argv[1], "rb").read().decode("utf-8", "replace")
sys.stdout.write(re.sub(
    r"\x1b\][^\x07\x1b]*(\x07|\x1b\\\\)|\x1b\[[0-9;?<>=]*[a-zA-Z]|\x1b[=>]|[\x00-\x08\x0b-\x1f]",
    "", raw))' "$1"
}

FAILED=0
assert_contains() { # file label needle...
    local file=$1 label=$2; shift 2
    for needle in "$@"; do
        if ! grep -qF -- "$needle" "$file"; then
            echo "FAIL($label): missing '$needle'"
            FAILED=1
        fi
    done
}

echo "== TUI flow: picker -> chat -> send -> save -> quit"
OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/cap.raw" "$TMP" \
    "1.5:\r" "1.5:hello from e2e" "1.5:\r" "3:\x13" "1.5:\x1b" "1.5:\x1b"
strip_ansi "$TMP/cap.raw" > "$TMP/cap.txt"
assert_contains "$TMP/cap.txt" tui \
    "gpt-5.6-luna" "hello from e2e" "fake reply done" "saved "
assert_contains "$TMP/reqlog.txt" request \
    '"model":"gpt-5.6-luna"' "hello from e2e"
TRANSCRIPT=$(ls "$OOLONG_TRANSCRIPT_DIR"/oolong-chat-*.md 2>/dev/null | head -1 || true)
if [ -z "$TRANSCRIPT" ]; then
    echo "FAIL(transcript): no file saved to $OOLONG_TRANSCRIPT_DIR"
    FAILED=1
else
    assert_contains "$TRANSCRIPT" transcript "<!--oolong:user-->" "hello from e2e"
fi

echo "== one-shot pipe mode"
echo "package main" | "$TMP/oolong" "explain" > "$TMP/oneshot.out"
assert_contains "$TMP/oneshot.out" oneshot "fake reply done"
tail -1 "$TMP/reqlog.txt" > "$TMP/lastreq.txt"
assert_contains "$TMP/lastreq.txt" oneshot-request 'package main\n\nexplain'

if [ "$FAILED" -ne 0 ]; then
    echo; echo "== captured frames (ANSI stripped) =="
    cat "$TMP/cap.txt"
    echo; echo "== request log =="
    cat "$TMP/reqlog.txt" 2>/dev/null || true
    exit 1
fi
echo "OK: all assertions passed"
