#!/bin/sh
# Sync the working tree to claude-machine-02, commit and push from there, then
# bring the laptop back in line.
#
#   sh hack/commit-from-box.sh "feat(thing): what changed"
#   sh hack/commit-from-box.sh -F /path/to/message.txt
#
# Why this exists: commits originate on the box, so that line endings are LF by
# construction rather than by cleanup. But files get authored on the laptop,
# which leaves the laptop holding changes that are already committed remotely.
# Pulling then refuses, and the obvious fix, discarding local changes, is one
# typo away from throwing away work that was never synced.
#
# So this verifies the box has exactly what the laptop has before discarding
# anything, and refuses if they differ.
set -eu

BOX="${COMPONIUM_BOX:-claude-machine-02.home}"
USER_AT="${COMPONIUM_USER:-claude}@$BOX"
KEY="${COMPONIUM_KEY:-$HOME/.ssh/siberian_debian}"
REMOTE_DIR="${COMPONIUM_DIR:-Componium}"

if [ $# -eq 0 ]; then
  echo "usage: $0 \"commit message\"   or   $0 -F messagefile" >&2
  exit 2
fi

echo "syncing working tree to $BOX"
tar --exclude=.git --exclude=node_modules -cf - . \
  | ssh -i "$KEY" "$USER_AT" "tar -xf - -C $REMOTE_DIR"

echo "committing on the box"
if [ "$1" = "-F" ]; then
  ssh -i "$KEY" "$USER_AT" "cd $REMOTE_DIR && git add -A && git commit -q -F -" < "$2"
else
  ssh -i "$KEY" "$USER_AT" "cd $REMOTE_DIR && git add -A && git commit -q -m \"$1\""
fi

echo "pushing"
ssh -i "$KEY" "$USER_AT" "cd $REMOTE_DIR && git push -q origin main && git log --oneline -1"

echo "bringing the laptop back in line"
git fetch -q origin
# Refuse to discard anything the box does not already have. A local file that
# differs from what was just pushed means the sync missed it, and destroying it
# would be the worst possible outcome of a convenience script.
for f in $(git diff --name-only); do
  if ! MSYS_NO_PATHCONV=1 git show "origin/main:$f" 2>/dev/null | diff -q - "$f" >/dev/null 2>&1; then
    echo "refusing to continue: $f differs from what is on origin/main" >&2
    echo "the sync did not include it, or something changed after the push" >&2
    exit 1
  fi
done
git checkout -- . 2>/dev/null || true
git pull -q --ff-only
echo "laptop at $(git log --oneline -1)"
