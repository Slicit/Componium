# Vendored

`three.module.min.js` and `OrbitControls.js` from three.js r169, under the MIT
licence in `three-LICENSE.txt`. Copyright the three.js authors.

They are committed rather than installed. The studio has no build step and no
`node_modules`, and the reason has not changed: the cost of a JavaScript
toolchain falls on every contributor, including the one who only wanted to fix
a cue time. Two files and a licence is a smaller price than that.

About 720 KB, served once and cached by content hash like every other asset.

To update, replace both files from the same release and update the version
here. They are loaded through an import map in `index.html`, so nothing else
needs to change.
