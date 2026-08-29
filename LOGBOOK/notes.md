# Notes

Codebase learnings, patterns, and anti-patterns. Add entries as they are
discovered, with the date.

## Patterns

## Gotchas

### 2026-08-29 · Line endings are LF. Everywhere. Always.

Everything runs on Linux. CRLF must never enter this repository, in any file
type, for any reason.

The trap is that CRLF is introduced **at the moment a file is written on the
Windows laptop**, not during sync or checkout. A file authored locally has CRLF
on every line before git ever sees it. The symptom on the box is a script that
fails with a confusing `not found` error, or firmware build steps that break
for no visible reason, and a plain read of the file shows nothing wrong.

Three layers defend against it, and all three must stay in place:

1. `.gitattributes` pins `* text=auto eol=lf`, so the committed blob is
   normalised even when the working file is not. This has been verified: the
   blobs in the genesis commit are clean LF despite every working file having
   CRLF at the time.
2. `.editorconfig` sets `end_of_line = lf` for editors and tooling that honour
   it.
3. Global git config on the Windows laptop pins `core.autocrlf=false` and
   `core.eol=lf`. There was previously no line ending configuration at all,
   which is why nothing normalised on checkout.

When writing files locally with shell heredocs, strip carriage returns before
committing, or the working tree drifts from the index:

```sh
find . -path ./.git -prune -o -type f -print | xargs sed -i 's/\r$//'
```

To check the whole tree at any point:

```sh
find . -path ./.git -prune -o -type f -print | xargs grep -lU $'\r'
```

### 2026-08-29 · Containers leave root owned files in bind mounted checkouts

Running a container as root against a bind mounted checkout leaves root owned
files behind (this has bitten the sibling projects repeatedly, in `/tmp`, as
`__pycache__`, and across Rails bootsnap cache directories). A plain `rm -rf`
then fails on hundreds of them, and under `set -e` a script dies there and
silently skips every later step. Use `sudo rm -rf`, and prefer running
containers as the checkout owner in the first place.

## Anti-patterns
