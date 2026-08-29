#!/bin/sh
# Build the clips the manual end to end checks use.
#
# These live in /tmp, which Debian cleans periodically, so this exists to make
# regenerating them a single command rather than an archaeology exercise.
#
#   sh hack/make-testclips.sh
set -eu

DIR="${1:-/tmp}"
PLAIN="$DIR/componium-testclip.mkv"
SUBBED="$DIR/componium-subbed.mkv"

if [ ! -f "$PLAIN" ]; then
  echo "building $PLAIN (120s, 24fps, 440Hz tone)"
  ffmpeg -v error -y \
    -f lavfi -i testsrc2=size=640x360:rate=24 \
    -f lavfi -i sine=frequency=440 \
    -t 120 -c:v libx264 -preset ultrafast -c:a aac "$PLAIN"
fi

if [ ! -f "$SUBBED" ]; then
  echo "building $SUBBED (the same clip, with an SDH subtitle track)"
  SRT="$DIR/componium-test.srt"
  cat > "$SRT" <<'SRTEOF'
1
00:00:05,000 --> 00:00:07,000
I do not like the look of that sky.

2
00:00:12,100 --> 00:00:14,000
[thunder rumbles]

3
00:00:20,000 --> 00:00:22,000
(rain patters on the roof)

4
00:00:30,500 --> 00:00:32,000
[explosion]

5
00:00:45,000 --> 00:00:47,000
[wind howling]
SRTEOF
  ffmpeg -v error -y -i "$PLAIN" -i "$SRT" -map 0 -map 1 -c copy -c:s srt "$SUBBED"
fi

echo "ready:"
ls -la "$PLAIN" "$SUBBED"
