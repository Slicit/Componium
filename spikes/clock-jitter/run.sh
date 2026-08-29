#!/bin/sh
# Generates a test clip, plays it headless, and measures the media clock.
# Run from the repository root:  sh spikes/clock-jitter/run.sh -duration 60s
set -eu

SOCK=/tmp/mpv-componium.sock
CLIP=/tmp/componium-testclip.mkv

if [ ! -f "$CLIP" ]; then
  echo "generating a 120s test clip..."
  ffmpeg -v error -y \
    -f lavfi -i testsrc2=size=640x360:rate=24 \
    -f lavfi -i sine=frequency=440 \
    -t 120 -c:v libx264 -preset ultrafast -c:a aac "$CLIP"
fi

rm -f "$SOCK"
# null video and audio outputs, so this runs headless. mpv still paces
# playback in realtime, which is what we are measuring against.
mpv --no-config --vo=null --ao=null --loop=inf \
    --input-ipc-server="$SOCK" "$CLIP" >/tmp/mpv-componium.log 2>&1 &
MPV=$!
trap 'kill $MPV 2>/dev/null || true' EXIT

i=0
while [ ! -S "$SOCK" ] && [ $i -lt 50 ]; do i=$((i+1)); sleep 0.1; done
if [ ! -S "$SOCK" ]; then
  echo "mpv never created $SOCK. Log:"; cat /tmp/mpv-componium.log; exit 1
fi

go run ./spikes/clock-jitter -socket "$SOCK" "$@"
