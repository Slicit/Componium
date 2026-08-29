# The studio, rebuilt

React and TypeScript, served at `/v2` while the original studio keeps working
at `/`. Both read the same API, so the two can be compared on the same score
rather than one replacing the other on trust.

    npm ci
    npm test          # the core and the renderer, in node
    npm run build     # writes ../internal/studio/webdist, which Go embeds

`npm run dev` proxies `/api` and `/media` to a studio on port 8799, so the
front end hot reloads against real scores:

    componium studio -score some.componium -media ~/films -addr 127.0.0.1:8799
    npm run dev

## Why the build output is committed

`internal/studio/webdist` is in git. That is the same bargain as vendoring
three.js: a plain `go build ./...` keeps producing a working studio, and a
contributor who only touches Go never needs npm installed. The whole cost of
it is a bundle that can drift from its source, and CI rebuilds and diffs it on
every push to stop exactly that.

## The three layers

    src/core      no DOM, no React, no dependencies. Time and frames, the
                  visible window, the score model, row layout. Exhaustively
                  tested in node.
    src/render    a score and a window in, a list of drawing primitives out.
                  Also testable in node — which is the point, because a canvas
                  that draws nothing looks exactly like one that works.
    src/ui        React. Owns pixels, pointers and the DOM, and nothing else.

The split is a correctness strategy rather than a tidiness one. The browser
this is developed against blocks subresources on LAN origins and never
delivers animation frames, so anything that can only be checked by looking at
a running page is effectively unchecked. Keeping that layer small is how the
bugs stay findable.

## Why jsdom is pinned

`jsdom` 26 and later need a Node that has `webidl.util.markAsUncloneable`,
which Node 20 does not have — it fails at import with a stack trace from inside
undici rather than anything resembling a version error. Both this project's
build box and CI run Node 20, so jsdom stays on 25 until that moves.
