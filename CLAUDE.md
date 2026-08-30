Read LOGBOOK.md

## Buttons: icons for the universal, words for the rest

An action with a settled, universal icon uses the icon — delete is a bin, save
a disk, edit a pencil, search a magnifier, paging chevrons. An action without
one keeps its word: Rebuild, Prepare, Reset and Resume have no glyph two people
read the same way, and inventing one produces a row of symbols that must all be
hovered to be understood.

A mixed row is the signal, not an inconsistency: icons are what you do often
and recognise instantly, words are what is worth reading before clicking.

Glyphs are inline SVG in web/src/ui/Icon.tsx — a handful is not worth a
dependency, and an icon font that fails to load leaves squares. Every icon
button carries an aria-label and a title; the svg itself is aria-hidden, or a
screen reader says it twice.
