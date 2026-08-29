#!/bin/sh
# Build the studio from the working tree, render a page of it with headless
# Chrome, and leave a PNG behind.
#
#   sh hack/shoot-studio.sh out.png [score] [port]
#
# Why this exists: the sandboxed browser this project is usually developed
# against blocks every subresource on a LAN origin, so the page loads and not
# one line of the application runs. It also never delivers animation frames.
# Between them, "open it and look" is not available, and a WebGL view that
# draws nothing looks exactly like one that works.
#
# Chrome with SwiftShader does render, in software, without a GPU or a display.
# It is slow and it is not what a user's machine will do, but it is a real
# browser resolving a real import map and running real three.js, and it
# produces an image somebody can actually look at. That is the difference
# between believing the room works and knowing it.
set -eu

OUT="${1:-/tmp/studio.png}"
SCORE="${2:-examples/demo.componium}"
PORT="${3:-8799}"
SIZE="${SHOT_SIZE:-1500,1000}"

command -v google-chrome >/dev/null || { echo "google-chrome not installed" >&2; exit 1; }

# Kill by port, not by a pid file and never by name.
#
# A pid file goes stale the moment a start fails, and the next run then kills
# nothing, binds nothing, and screenshots whatever ancient build is still
# holding the port. That happened, twice, and both times the screenshot looked
# plausible and was of the wrong binary. Matching on the process name is worse:
# `pkill -f "componium studio"` also matches the ssh command that contains that
# string, which is how a previous session killed its own connection.
if command -v fuser >/dev/null 2>&1; then
  fuser -k "$PORT/tcp" 2>/dev/null || true
else
  ss -ltnp 2>/dev/null | awk -v p=":$PORT" '$4 ~ p {print}' \
    | grep -o 'pid=[0-9]*' | cut -d= -f2 | xargs -r kill 2>/dev/null || true
fi
sleep 1

export PATH="$PATH:/usr/local/go/bin"
go build -o /tmp/componium-shot ./cmd/...

/tmp/componium-shot studio \
  -score="$SCORE" \
  -rig=deploy/demo-rig.toml \
  -media="${COMPONIUM_MEDIA:-/home/claude/componium-media}" \
  -scores="${COMPONIUM_SCORES:-/home/claude/componium-scores}" \
  -addr="127.0.0.1:$PORT" >/tmp/componium-shot.log 2>&1 &
SHOT_PID=$!
trap 'kill $SHOT_PID 2>/dev/null || true' EXIT

# Wait for the port rather than sleeping a guess, and fail loudly if it never
# comes up. A screenshot of a connection error is still a valid PNG.
i=0
until curl -sf -o /dev/null "http://127.0.0.1:$PORT/"; do
  i=$((i + 1))
  [ "$i" -gt 40 ] && { echo "studio did not start:" >&2; cat /tmp/componium-shot.log >&2; exit 1; }
  sleep 0.25
done

# The asset hash proves which build answered. Two screenshots of an unnoticed
# stale process are what made this line necessary.
echo "serving $(curl -s "http://127.0.0.1:$PORT/" | grep -o 'room3d.js?v=[a-f0-9]*' | head -1)"

# --enable-unsafe-swiftshader is the flag that gets WebGL without a GPU. The
# virtual time budget lets the page finish fetching, parse three.js and draw a
# few frames before the shutter; too short and the room is genuinely empty.
timeout 180 google-chrome \
  --headless --no-sandbox --disable-dev-shm-usage \
  --enable-unsafe-swiftshader --hide-scrollbars \
  --window-size="$SIZE" --virtual-time-budget=9000 \
  --screenshot="$OUT" "http://127.0.0.1:$PORT/" 2>/dev/null

ls -l "$OUT"
