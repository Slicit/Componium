# Notes

Codebase learnings, patterns, and anti-patterns. Add entries as they are
discovered, with the date.

## Patterns

## Gotchas

### 2026-08-29 · Windows checkouts reintroduce CRLF

`.gitattributes` pins `* text=auto eol=lf` from the first commit. Authoring
happens on Windows and execution on Debian, and CRLF silently breaks shell
scripts and firmware build steps. If a script fails on the box with a
confusing "not found" error, check line endings first.

### 2026-08-29 · Containers leave root owned files in bind mounted checkouts

Running a container as root against a bind mounted checkout leaves root owned
files behind (this has bitten the sibling projects twice, once as `__pycache__`
in the tree). Prefer running as the checkout owner.

## Anti-patterns
