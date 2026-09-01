---
status: shipped
branch: fix-effect-parity
---

# fix-effect-parity · the library and the timeline say the same thing

Reported as: insert Strobe at the playhead, get one light event with no
intensity and no points. Three separate faults, all of them the same shape, all
of them a preset being mistranslated on the way into a track.

## What was actually wrong

**A repeating shape collapsed into one cue.** A cue track was assumed to want
one dose, which is right for a fogger (you tell it how hard and for how long,
and the shape inside the burst is its business) and wrong for a lamp that can
flash. `stutter(12)` is not a two second flash held at full. It is twelve
flashes, and one of them is a different preset wearing the same name.

**Channels came from the kind, not the track.** `channelsOf` read `track.points`
and nothing else, so a cue track, which has no points at all, always fell
through to the kind's default names. The composer writes light events as
`h`/`s`/`i`; the default is `r`/`g`/`b`. Every insert into `light.event` landed
in a vocabulary the rest of the track did not speak, which is why the editor
had no intensity to offer: it offers the lanes the track uses, and the new cue
used different ones.

**A level was written into a hue.** The peak went into every channel a track
named. On `r`/`g`/`b` that is white at full and defensible. On `h`/`s`/`i` it is
a saturated red, and a Fade up on `light.ambient` swept the hue through the
spectrum on its way to bright.

And one hole the same audit found: five motion presets were offered for cue
tracks, where `build` correctly refuses to invent a verb for a platform. The
picker offered them; the insert declined; nothing happened and nothing said so.

## The rule

**Anything the picker offers must insert, into the track it was offered for,
something that keeps the shape's pulses and lands in that track's own
channels.**

Four parts, in the order they bite:

1. **Pulses survive.** A cue track gets one cue per stretch of the shape above
   zero. One gesture stays one cue; twelve pulses arrive as twelve.
2. **Channels are the track's.** `channelsOf` reads points *and* cues. What is
   already written in a track decides what gets written into it.
3. **The number is a level.** Where a track names one (`i`, `intensity`,
   `output`), the shape drives that alone and the rest hold what the curve had
   under the playhead. Where it names none, every channel takes it.
4. **Offered means buildable.** A kind with no cue action gets no cue presets.
   Silence in the picker beats a button that does nothing.

## Where the rule lives

`web/src/core/parity.test.ts`, not here and not in a comment. It walks every
preset against every kind it declares against both track types and asserts all
four, in both directions. Adding a preset, a kind, an action or a channel
vocabulary means the test walks it without anybody remembering to.

The both-directions part is the load bearing half. Asserting only that what is
offered can be built is satisfied by offering nothing, so it also asserts that
everything *not* offered would genuinely have been refused.

## Decisions

- **2026-09-01 · One cue per pulse rather than a `repeats` flag on the preset.**
  A flag is a second place to be wrong, and it would have to be set correctly on
  every future preset by hand. The shape already knows: a stretch above zero is
  a pulse, and counting them needs no new field. Single gesture presets come out
  of it unchanged, which is why nothing else in the library moved.
- **2026-09-01 · No `motion` action invented.** `ACTION_BY_KIND` has no entry
  for motion because there is no truthful verb for a platform; you do not tell
  it to "move", you tell it where to be. The fix was to stop offering motion
  presets on cue tracks, not to make up a word for an instrument to ignore.
- **2026-09-01 · The base holds colour, and only colour.** Feeding the track's
  value under the playhead into the level channel too would have turned every
  insert into a blend with the curve it replaces. An existing test caught that
  within a minute of it being written, which is the argument for the test file
  in one sentence.
