---
status: shipped
branch: fix-frame-clock
---

# fix-frame-clock · the playhead runs at the film's rate

Reported as a strobe that does not line up with the room, with the reasonable
guess that the 3D view cannot keep up. It can. It was being handed four light
values a second.

## The measurement that was not needed

The room renders on an animation frame loop and a frame costs single digit
milliseconds. It has capacity to spare. The playhead came from the video
element's `timeupdate` event, which every browser throttles to roughly four a
second, and the room is updated by an effect keyed on that value.

A strobe is twelve pulses over two seconds: a 167ms period, 83ms on, 83ms off.
That is a 6Hz square wave sampled at 4Hz, far below Nyquist, so it cannot be
reconstructed at all. What reaches the eye is a handful of flashes at arbitrary
moments, landing differently on every pass.

The film itself looked smooth through all of this, which is what made the room
look like the guilty party. three.js registers `requestVideoFrameCallback` for
its video texture, so the television and the projection were already stepping
per presented frame. Only the score was not.

## What it does now

`requestVideoFrameCallback` fires once per frame the browser actually presents
and hands over that frame's own presentation time. That is the film's clock
rather than a sampling of it, and it matters twice: the score steps at the rate
the film does, and it steps at the same instants the picture does. Animation
frames are the fallback, which is the display's rate rather than the film's:
more often than a 24p film needs and never less.

## Decisions

- **2026-09-01 · `mediaTime`, not `currentTime`.** They are different numbers:
  one is the timestamp of the frame on screen, the other is where the element
  has got to, which is slightly later. Feeding the playhead from both makes it
  step backwards several times a second, so while the film runs the frames own
  the clock and `timeupdate` is ignored. It still serves the stopped case,
  where there are no frames to present.
- **2026-09-01 · The callback is held through a ref.** `follow` is rebuilt
  whenever the viewport changes, which is once per scroll, and a loop torn down
  and restarted mid-scroll drops frames for a reason that has nothing to do
  with the film.
- **2026-09-01 · Tested through `App.test.tsx`.** This is pure wiring, and the
  file already exists for exactly that: its opening comment is about a listener
  attached to an element that had not mounted yet, invisible to every unit
  test in the suite. jsdom has no `requestVideoFrameCallback`, so the element
  is given one, which is also what a browser does.

## The counter in the corner

Added alongside, and it reports two numbers rather than one because the room
draws on demand: the rate is frames actually drawn, the cost is how long one
took to build. A low rate beside a small cost is a room being asked for frames
slowly. A high cost is a room that cannot keep up. Opposite problems, and a
single FPS number cannot tell them apart, which is the whole reason this
feature was misdiagnosed in the first place.
