#!/bin/sh
# Build a clip that actually exercises the analysis engine.
#
# The plain test clip is a scrolling test pattern with a constant tone. It is
# fine for timing work and useless for anything that asks what the film is
# doing: it is uniformly loud, uniformly busy, and permanently moving downward,
# which the plunge detector correctly but uselessly reports thirty times.
#
# This one has structure:
#
#   0-25s    calm      dark, near silent
#   25-40s   flashes   black with five bright spikes
#   40-60s   plunge    fast downward scroll, loud low end
#   60-80s   calm      dark, near silent again
#
# So calm detection should find two regions, flash detection five events, and
# plunge detection one run. Anything else is the engine being wrong rather than
# the fixture being strange.
set -eu

DIR="${1:-/tmp}"
OUT="$DIR/componium-dynamics.mkv"
WORK="$DIR/componium-dyn-parts"

[ -f "$OUT" ] && { echo "$OUT already exists"; exit 0; }
mkdir -p "$WORK"
V="-c:v libx264 -preset ultrafast -pix_fmt yuv420p"
A="-c:a aac -ar 48000 -ac 1"

echo "calm segments"
for part in calm1 calm2; do
  ffmpeg -v error -y -f lavfi -i "color=c=0x0a0f18:s=640x360:r=24" \
    -f lavfi -i "sine=frequency=200:sample_rate=48000" \
    -af "volume=0.02" -t 20 $V $A "$WORK/$part.mkv"
done

echo "flash segments"
ffmpeg -v error -y -f lavfi -i "color=c=0x0a0f18:s=640x360:r=24" \
  -f lavfi -i "sine=frequency=200:sample_rate=48000" \
  -af "volume=0.05" -t 2.85 $V $A "$WORK/dark.mkv"
ffmpeg -v error -y -f lavfi -i "color=c=white:s=640x360:r=24" \
  -f lavfi -i "sine=frequency=90:sample_rate=48000" \
  -af "volume=0.9" -t 0.15 $V $A "$WORK/white.mkv"

echo "plunge segment: fast downward scroll with a loud low end"
ffmpeg -v error -y -f lavfi -i "testsrc2=s=640x360:r=24" \
  -f lavfi -i "sine=frequency=45:sample_rate=48000" \
  -vf "scroll=vertical=0.02" -af "volume=0.9" \
  -t 20 $V $A "$WORK/plunge.mkv"

# Concat demuxer rather than the filter: every part already has identical
# codecs and parameters, so this is a copy rather than a re-encode.
LIST="$WORK/list.txt"
: > "$LIST"
echo "file '$WORK/calm1.mkv'" >> "$LIST"
i=0
while [ $i -lt 5 ]; do
  echo "file '$WORK/dark.mkv'"  >> "$LIST"
  echo "file '$WORK/white.mkv'" >> "$LIST"
  i=$((i + 1))
done
echo "file '$WORK/plunge.mkv'" >> "$LIST"
echo "file '$WORK/calm2.mkv'"  >> "$LIST"

ffmpeg -v error -y -f concat -safe 0 -i "$LIST" -c copy "$OUT"
rm -rf "$WORK"
echo "ready: $OUT"
ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 "$OUT"
