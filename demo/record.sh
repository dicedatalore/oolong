#!/usr/bin/env bash
# Record demo/demo.gif with Charm VHS against the fake API from e2e/ —
# deterministic, fake-credential-only, no real network. Needs vhs on PATH (plus its
# ttyd/ffmpeg dependencies); the demo workflow installs those in CI.
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
mkdir -p "$TMP/bin"
# Stamp the latest release tag so the banner doesn't show a pseudo-version
# with a -dirty suffix; goreleaser stamps real releases the same way.
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo dev)
# CGO off: the demo never pastes an image, and this keeps the build
# dependency-free on CI runners.
CGO_ENABLED=0 go build \
    -ldflags "-X github.com/dicedatalore/oolong/internal/version.Version=$VERSION" \
    -o "$TMP/bin/oolong" .
go build -o "$TMP/fakeapi" ./e2e/fakeapi

echo "== start fake API"
REPLY_FILE="$PWD/demo/reply.md" REPLY_DELAY_MS=40 \
    "$TMP/fakeapi" 127.0.0.1:0 > "$TMP/fakeapi.out" &
FAKEAPI_PID=$!
ADDR=""
for _ in $(seq 50); do
    ADDR=$(sed -n 's/^listening on //p' "$TMP/fakeapi.out")
    [ -n "$ADDR" ] && break
    sleep 0.1
done
[ -n "$ADDR" ] || { echo "fakeapi never came up"; exit 1; }
echo "   fakeapi on $ADDR"

# The tape just types "oolong": the fresh build is first on PATH, and the
# env isolates it from any real key, config, or transcript directory.
export PATH="$TMP/bin:$PATH"
export OPENAI_API_KEY=sk-test
export ANTHROPIC_API_KEY=sk-ant-test
export OPENAI_BASE_URL="http://$ADDR/v1"
export XDG_CONFIG_HOME="$TMP/xdg"
export OOLONG_TRANSCRIPT_DIR="$TMP"

# Keep the demo catalog compact while showing provider-aware routing. The
# tape selects Claude, whose native Messages API is served by fakeapi.
mkdir -p "$XDG_CONFIG_HOME/oolong"
cat > "$XDG_CONFIG_HOME/oolong/config.toml" <<EOF
[[models]]
id = "gpt-5.6-luna"
provider = "openai"
description = "Fast OpenAI model"

[[models]]
id = "claude-sonnet-5"
provider = "anthropic"
description = "Frontier intelligence at scale"
base_url = "http://$ADDR"
EOF

echo "== record"
cd demo
vhs demo.tape
echo "wrote demo/demo.gif"
