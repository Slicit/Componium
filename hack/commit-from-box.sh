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
# Two kinds of local change to clear: tracked files that were modified, and
# files that were untracked here but are tracked on origin now. Both are
# checked against what was just pushed before anything is removed. A local
# file that differs means the sync missed it, and destroying it would be the
# worst possible outcome of a convenience script.
same_as_origin() {
  MSYS_NO_PATHCONV=1 git show "origin/main:$1" 2>/dev/null | diff -q - "$1" >/dev/null 2>&1
}

for f in $(git diff --name-only); do
  if ! same_as_origin "$f"; then
    echo "refusing to continue: $f differs from what is on origin/main" >&2
    exit 1
  fi
done

# Untracked locally, tracked on origin: the pull would refuse to overwrite
# them. Remove only the ones origin already has byte for byte.
for f in $(git ls-files --others --exclude-standard); do
  if MSYS_NO_PATHCONV=1 git show "origin/main:$f" >/dev/null 2>&1; then
    if same_as_origin "$f"; then
      rm -f "$f"
    else
      echo "refusing to continue: untracked $f differs from origin/main" >&2
      exit 1
    fi
  fi
done
# Move to origin in one step, now that every local file has been verified
# to match it. The obvious alternative, git checkout followed by git pull,
# is dangerous: checkout reverts to *local* HEAD, so if the pull then fails
# for any reason, the working tree has been reset to a commit older than
# what was just pushed. The next sync propagates that as a reversion.
#
# That is not hypothetical. It happened on 2026-08-29 and silently undid
# 346 lines of a feature in a commit whose message said it was fixing this
# script.
git reset --hard origin/main >/dev/null
echo "laptop at $(git log --oneline -1)"
