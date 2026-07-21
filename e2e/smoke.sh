#!/usr/bin/env bash
# End-to-end release smoke test: first run, key validation, save/resume,
# resize, cancellation, and all four provider paths against a fake API.
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
go build -tags=e2e -o "$TMP/oolong" .
go build -o "$TMP/fakeapi" ./e2e/fakeapi

echo "== start fake API"
REQLOG="$TMP/reqlog.txt" \
ANTHROPIC_REQLOG="$TMP/anthropic-reqlog.txt" \
GEMINI_REQLOG="$TMP/gemini-reqlog.txt" \
OLLAMA_REQLOG="$TMP/ollama-reqlog.txt" \
    "$TMP/fakeapi" 127.0.0.1:0 > "$TMP/fakeapi.out" &
FAKEAPI_PID=$!
for _ in $(seq 50); do
    ADDR=$(sed -n 's/^listening on //p' "$TMP/fakeapi.out")
    [ -n "$ADDR" ] && break
    sleep 0.1
done
[ -n "$ADDR" ] || { echo "fakeapi never came up"; exit 1; }
echo "   fakeapi on $ADDR"

export OPENAI_API_KEY=sk-test
export ANTHROPIC_API_KEY=sk-ant-test
export GEMINI_API_KEY=AIza-test
export OPENAI_BASE_URL="http://$ADDR/v1"
export XDG_CONFIG_HOME="$TMP/xdg"
export OOLONG_TRANSCRIPT_DIR="$TMP/transcripts"
export TERM=xterm-256color

mkdir -p "$XDG_CONFIG_HOME/oolong"
cat > "$XDG_CONFIG_HOME/oolong/config.toml" <<EOF
[[models]]
id = "gpt-5.6-luna"
provider = "openai"
description = "OpenAI smoke model"

[[models]]
id = "claude-sonnet-5"
provider = "anthropic"
description = "Anthropic smoke model"
base_url = "http://$ADDR"

[[models]]
id = "gemini-3.5-flash"
provider = "google"
description = "Gemini smoke model"
base_url = "http://$ADDR"

[[models]]
id = "gemma3"
provider = "ollama"
description = "Ollama smoke model"
base_url = "http://$ADDR"
EOF

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

echo "== first run: empty setup guidance"
mkdir -p "$TMP/first-run-xdg"
env -u OPENAI_API_KEY -u ANTHROPIC_API_KEY -u GEMINI_API_KEY -u OPENAI_BASE_URL \
    XDG_CONFIG_HOME="$TMP/first-run-xdg" OOLONG_BIN="$TMP/oolong" \
    python3 e2e/drive.py "$TMP/first-run.raw" "$TMP" "1.5:\x03"
strip_ansi "$TMP/first-run.raw" > "$TMP/first-run.txt"
assert_contains "$TMP/first-run.txt" first-run \
    "Welcome to Oolong" "ctrl+k" "oolong config init" "OS keychain"

echo "== failed key validation"
env -u OPENAI_API_KEY -u ANTHROPIC_API_KEY -u GEMINI_API_KEY \
    OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/key-failure.raw" "$TMP" \
    "0.8:\x0b" "0.4:\t" "0.2:bad-key" "1.5:\r" "0.5:\x1b" "0.5:\x03"
strip_ansi "$TMP/key-failure.raw" > "$TMP/key-failure.txt"
assert_contains "$TMP/key-failure.txt" key-failure \
    "Anthropic key wasn't accepted" "check it and try again"

echo "== TUI flow: picker -> chat -> send -> save -> quit"
OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/cap.raw" "$TMP" \
    "1.5:\r" "1.5:hello from e2e" "1.5:\r" "2:@resize=18x72" \
    "0.8:@resize=30x100" "0.8:\x13" "1.5:\x1b" "1.5:\x1b"
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
    assert_contains "$TRANSCRIPT" transcript "# Oolong chat — gpt-5.6-luna" "## You" "hello from e2e"
    if grep -qF -- "<!--" "$TRANSCRIPT"; then
        echo "FAIL(transcript): contains hidden metadata"
        FAILED=1
    fi
fi

if [ -n "$TRANSCRIPT" ]; then
    echo "== resume saved transcript"
    OOLONG_ARGS="--resume $TRANSCRIPT" OOLONG_BIN="$TMP/oolong" \
        python3 e2e/drive.py "$TMP/resume.raw" "$TMP" "1.5:\x03"
    strip_ansi "$TMP/resume.raw" > "$TMP/resume.txt"
    assert_contains "$TMP/resume.txt" resume \
        "gpt-5.6-luna" "hello from e2e" "fake reply done"
fi

echo "== cancel an in-flight response"
REPLY_DELAY_MS=500 OOLONG_BIN="$TMP/oolong" \
    python3 e2e/drive.py "$TMP/cancel.raw" "$TMP" \
    "1:\r" "0.5:cancel this response" "0.2:\r" "0.3:\x1b" "1:\x03"
strip_ansi "$TMP/cancel.raw" > "$TMP/cancel.txt"
assert_contains "$TMP/cancel.txt" cancellation "cancel this response" "Send a message"
assert_contains "$TMP/reqlog.txt" cancellation-request "cancel this response"

echo "== one-shot pipe mode"
echo "package main" | "$TMP/oolong" "explain" > "$TMP/oneshot.out"
assert_contains "$TMP/oneshot.out" oneshot "fake reply done"
tail -1 "$TMP/reqlog.txt" > "$TMP/lastreq.txt"
assert_contains "$TMP/lastreq.txt" oneshot-request 'package main\n\nexplain'

echo "== Anthropic TUI flow: picker -> Claude -> chat -> send -> quit"
OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/anthropic-cap.raw" "$TMP" \
    "1.5:\x1b[B" "0.5:\r" "1.5:hello anthropic e2e" "1.5:\r" "4:\x1b" "1:\x1b"
strip_ansi "$TMP/anthropic-cap.raw" > "$TMP/anthropic-cap.txt"
assert_contains "$TMP/anthropic-cap.txt" anthropic-tui \
    "claude-sonnet-5" "hello anthropic e2e" "fake reply done"
assert_contains "$TMP/anthropic-reqlog.txt" anthropic-request \
    '"model":"claude-sonnet-5"' "hello anthropic e2e"

echo "== Gemini TUI flow: picker -> Gemini -> chat -> send -> quit"
OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/gemini-cap.raw" "$TMP" \
    "1.5:\x1b[B" "0.5:\x1b[B" "0.5:\r" "1.5:hello gemini e2e" "1.5:\r" "3:\x1b" "1:\x1b"
strip_ansi "$TMP/gemini-cap.raw" > "$TMP/gemini-cap.txt"
assert_contains "$TMP/gemini-cap.txt" gemini-tui \
    "gemini-3.5-flash" "hello gemini e2e" "fake reply done"
assert_contains "$TMP/gemini-reqlog.txt" gemini-request "hello gemini e2e"

echo "== Gemini one-shot mode"
"$TMP/oolong" --model gemini-3.5-flash "gemini one shot" > "$TMP/gemini-oneshot.out"
assert_contains "$TMP/gemini-oneshot.out" gemini-oneshot "fake reply done"
tail -1 "$TMP/gemini-reqlog.txt" > "$TMP/gemini-lastreq.txt"
assert_contains "$TMP/gemini-lastreq.txt" gemini-oneshot-request "gemini one shot"

echo "== Ollama TUI flow: picker -> Ollama -> chat -> send -> quit"
OOLONG_BIN="$TMP/oolong" python3 e2e/drive.py "$TMP/ollama-cap.raw" "$TMP" \
    "1.5:\x1b[B" "0.3:\x1b[B" "0.3:\x1b[B" "0.5:\r" \
    "1:hello ollama e2e" "0.5:\r" "3:\x1b" "0.5:\x1b"
strip_ansi "$TMP/ollama-cap.raw" > "$TMP/ollama-cap.txt"
assert_contains "$TMP/ollama-cap.txt" ollama-tui \
    "gemma3" "hello ollama e2e" "fake reply done"
assert_contains "$TMP/ollama-reqlog.txt" ollama-request \
    '"model":"gemma3"' "hello ollama e2e"

echo "== Ollama one-shot mode"
"$TMP/oolong" --model gemma3 "ollama one shot" > "$TMP/ollama-oneshot.out"
assert_contains "$TMP/ollama-oneshot.out" ollama-oneshot "fake reply done"
tail -1 "$TMP/ollama-reqlog.txt" > "$TMP/ollama-lastreq.txt"
assert_contains "$TMP/ollama-lastreq.txt" ollama-oneshot-request "ollama one shot"

echo "== Anthropic one-shot mode"
"$TMP/oolong" --model claude-sonnet-5 "anthropic one shot" > "$TMP/anthropic-oneshot.out"
assert_contains "$TMP/anthropic-oneshot.out" anthropic-oneshot "fake reply done"
tail -1 "$TMP/anthropic-reqlog.txt" > "$TMP/anthropic-lastreq.txt"
assert_contains "$TMP/anthropic-lastreq.txt" anthropic-oneshot-request \
    '"model":"claude-sonnet-5"' "anthropic one shot"

if [ "$FAILED" -ne 0 ]; then
    echo; echo "== captured frames (ANSI stripped) =="
    cat "$TMP/cap.txt"
    echo; echo "== request log =="
    cat "$TMP/reqlog.txt" 2>/dev/null || true
    echo; echo "== Anthropic request log =="
    cat "$TMP/anthropic-reqlog.txt" 2>/dev/null || true
	 echo; echo "== Gemini request log =="
	 cat "$TMP/gemini-reqlog.txt" 2>/dev/null || true
	 echo; echo "== Ollama request log =="
	 cat "$TMP/ollama-reqlog.txt" 2>/dev/null || true
    exit 1
fi
echo "OK: all assertions passed"
