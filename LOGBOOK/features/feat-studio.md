---
status: shipped
branch: feat-studio
---

# Studio

## Intent

Authoring is the real user experience problem. A score is editable as text, but
seeing forty cues against a two hour timeline is not something a text editor
does well.

## Decisions

### 2026-08-29

- **Decision:** No framework and no build step. Plain HTML, CSS and JavaScript,
  embedded in the Go binary with `go:embed` and served by `componium studio`.
  This departs from the React plan recorded in LOGBOOK.md, which has been
  amended.
- **Why:** The editor is a few hundred lines of DOM and SVG. A JavaScript
  toolchain would impose `npm install` on every contributor who only wanted to
  fix a cue time, and this project's likely contributors are people wiring fans
  to microcontrollers. Embedding also means there is exactly one artifact to
  ship. If the editor outgrows this, the toolchain can be added then, and that
  decision will be better informed than it would be today.
- **Impact:** No bundler, no `node_modules`, no build step. The trade is no
  TypeScript and no component model.

### 2026-08-29

- **Decision:** Saving round trips through the real parser and writer before
  the file is touched.
- **Why:** The studio must never be able to produce a score `componium play`
  would refuse. Validating with a second implementation would mean two things
  to keep in agreement.
- **Impact:** An invalid edit is refused with the same message the player would
  give, and the file on disk is left exactly as it was. That has a test.

### 2026-08-29

- **Decision:** Fields the editor does not display are carried over from the
  previous score rather than rebuilt from the page.
- **Why:** The page never shows the media hash or the interpolation mode, so
  nothing in it can carry them, and anything not carried is silently destroyed
  on save. Losing the hash would break the binding between a score and its
  film, which is the thing that makes a shared score library possible.

### 2026-08-29

- **Decision:** Timecode handling lives in its own file with its own tests.
- **Why:** It is the only genuinely tricky logic in the front end, and it must
  agree exactly with the Go parser. A person typing a time in the studio should
  get the same result as typing it in the file, and a mismatch would stay
  invisible until a cue landed in the wrong place.

## Verification

Six Go tests over the API, including that an invalid edit leaves the file
untouched and that unshown fields survive a save. Seven front end tests run
under node with no framework.

**The page itself was not verified in a browser.** It was served successfully
and its HTML loaded, but this environment's browser pane blocks subresource
requests with `ERR_BLOCKED_BY_CLIENT`, so the stylesheet and script never
loaded and no rendering or interaction could be exercised. The API is proven,
the timecode logic is proven, the rendering and event handling are not.

## Links

- Branch: `feat-studio`
- Related features: `feat-score-format`
