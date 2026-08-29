# Buying the hardware

Componium runs completely without hardware: every instrument kind has a virtual
implementation, and `componium node` is a full software instrument over the
network. Nothing below is needed to develop, to author a score, or to see the
whole system work.

This is about the order to spend money in, if you want to.

## The principle

**Buy in order of how much each purchase teaches you**, not in order of how
impressive the effect is. Motion is the thing everyone wants first and the
thing to buy last: it is the most expensive, the most dangerous, and it
proves nothing the cheaper instruments have not already proven.

Every stage below is usable on its own. Stop at any point and you still have a
working rig.

## Stage 1, about €25: light

One addressable LED strip with a WLED controller, or a USB to DMX interface if
you already own fixtures.

This is the best first purchase by a distance. It is visible from across the
room, it cannot hurt anyone, and **a timing error is instantly obvious** in a
way it is not for anything else: light lands on the frame or it does not, and
your eye knows immediately.

It also exercises the whole chain end to end, since sACN is a real protocol
going to real hardware.

```toml
[[instrument]]
id = "light.ambient"
kind = "light"
driver = "sacn"
latency = "20ms"
universe = 1
start = 1
mode = "rgb"
```

Then generate a score from a film with the composer and watch the room follow
the picture. That alone is a good evening.

## Stage 2, about €15: an ESP32

Any ESP32 dev board. Flash `firmware/esp32`, or run `componium node` on a
spare machine first to see the protocol work before soldering anything.

This is where CIP becomes real, and where the node watchdog stops being a test
and starts being the thing that protects you.

## Stage 3, about €20: wind

A 120mm PWM fan, a 12V supply and a MOSFET module. Wire the fan's PWM pin to
the ESP32.

The first instrument with **real, large latency**, and therefore the first
honest test of the thing the whole project is built around. Measure the spin
up time yourself and put the real number in the manifest. Everything else
follows from that number being true.

## Stage 4, about €40: shake

A bass shaker or tactile transducer, plus a small amplifier.

Cheap, nearly risk free, and the first instrument that genuinely needs the
clock to be good: shake is felt against the picture, and tens of milliseconds
are perceptible. This is where the 3 ms clock stops being a number in a test
and starts being the reason it feels right.

## Stage 5, €50 and up: fog or mist

**Read [wet-and-hot.md](wet-and-hot.md) first.** A small fogger, or a mister
for rain scenes, on a relay driven by a node.

Dry run it: leave the machine unplugged and listen to the relay click, so you
can hear the duty cycle behave before anything is emitted. Set
`max_continuous_ms` and `duty_cycle` in the node's manifest before the first
real run, not after the first surprise.

## Stage 6, hundreds to thousands: motion

A DIY platform, or a commercial seat. Componium emits 6DOF pose and hands off
to whatever already drives your rig.

Set `travel` far smaller than the rig's real range until you trust the score,
and note that `componium play` reports how often it had to clamp. A score that
clamps constantly was written for a different rig.

Motion needs mechanical end stops and an emergency stop that is **not on the
network**. Software clamping is a last resort inside one process, not a safety
system.

## What this project cannot tell you

Every latency figure in the examples is invented. Nothing here has driven a
physical device, so the 20 ms for a DMX fixture and the 1.2 s for a fan are
plausible guesses rather than measurements.

Measure your own and put the real numbers in your manifests. If you write down
what you find, that is the single most useful thing you could contribute back.
