#!/bin/sh
# Wait for CI to finish for the commit that is actually checked out.
#
#   sh hack/wait-for-ci.sh
#
# The obvious version of this, "wait until the newest run is complete", is
# wrong and fails silently: for the first minute after a push there is no run
# for the new commit yet, so the newest run is the *previous* one, already
# green. It returns immediately, the deploy pulls a stale image, and the change
# appears not to have worked. That happened three times before this existed.
set -eu

REPO="${COMPONIUM_REPO:-Slicit/componium}"
SHA="$(git rev-parse HEAD)"
SHORT="$(echo "$SHA" | cut -c1-7)"
TRIES="${2:-90}"

echo "waiting for CI on $SHORT"
i=0
while [ "$i" -lt "$TRIES" ]; do
  # Match on the commit, not on recency. An empty result means the run has not
  # been created yet, which is a reason to keep waiting rather than to stop.
  status=$(gh run list --repo "$REPO" --workflow CI --limit 15 \
    --json status,conclusion,headSha \
    --jq ".[] | select(.headSha==\"$SHA\") | \"\(.status) \(.conclusion)\"" 2>/dev/null | head -1)

  case "$status" in
    "completed success") echo "CI green for $SHORT"; exit 0 ;;
    completed*)          echo "CI failed for $SHORT: $status" >&2; exit 1 ;;
    "")                  printf "." ;;
    *)                   printf "+" ;;
  esac
  sleep 20
  i=$((i + 1))
done

echo >&2
echo "gave up waiting for CI on $SHORT" >&2
exit 1
