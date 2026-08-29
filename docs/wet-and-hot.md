# Fog, water, heat and moving mass

Everything else in Componium can be got wrong and the worst outcome is a film
that feels odd. These four can hurt someone, damage a room, or start a fire.
This page is the reasoning behind why they are last in the roadmap and what the
software does about them.

Componium is software written by people who cannot see your rig. **Nothing here
substitutes for a physical interlock, a fuse, an RCD, or a person with their
hand on a switch.**

## Why they are last

Not because they are hard to code. Fog and water are a relay and a valve, which
is the easiest instrument to write in the whole project. They are last because
everything protecting them had to exist first: the clock that says when, the
conductor that says what, the safety supervisor that says stop, and the node
watchdog that stops without being asked.

Building a fogger driver in week one would have been quick and irresponsible.

## What can go wrong, and what answers it

**A fogger left running.** The classic failure. A fog machine that receives
"on" and never receives "off" empties itself in minutes, sets off smoke alarms,
and in a small room reduces visibility to nothing.

Three independent things prevent it, and all three should be configured:

- `MaxContinuous` in the instrument manifest, enforced by the safety supervisor.
- `DutyCycle` over a window, likewise.
- The node's own watchdog, which returns the output to its safe state 300 ms
  after heartbeats stop, without needing anyone to send a stop. This is the one
  that works when the conductor has crashed.

**Water near electricity.** Componium has nothing useful to say about this.
Misters near a DMX fixture or a mains lead are a wiring problem, and the answer
is physical: separation, drip loops, an RCD.

**A platform driven past its travel.** A motion rig commanded beyond its range
does not refuse, it drives into its end stops. `instruments/motion` clamps every
pose to the declared `travel` before sending it, counts how often it had to,
and defaults to deliberately tiny limits when a rig has not declared any: an
undeclared rig should move timidly rather than confidently.

Clamping is not a safety system. It is a last resort inside one process. A
platform needs mechanical end stops and an emergency stop that is not on the
network.

**Scent that will not go away.** Scent is the only effect here that cannot be
undone. A fan stops, a light goes dark, a platform returns to neutral; a smell
stays in the room for the rest of the film and often the rest of the evening.
It is also the effect most likely to affect somebody physically: asthma and
fragrance allergies are common and are not visible.

The composer maps only the most unambiguous words to scent, on purpose. Keep
puffs short, duty windows long, and ask before using it on guests.

**Heat.** Foggers and hazers contain a heater block. They should never be
driven immediately from cold, and Componium does not know whether yours is
warm. Nothing in the software models this; it is on the operator.

## What the software will not do

- It will not infer a duty cycle. If a manifest declares no limits, the
  supervisor enforces none, and that is on purpose: a guessed limit that is too
  generous is worse than an obvious absence.
- It will not restart a stopped rig on its own. All-stop latches until someone
  explicitly resets it, because the person who stopped it is probably standing
  next to it.
- It will not treat a generated score as playable. The composer's output says
  so in a comment at the top of every file. A model has no idea how much water
  your mister holds.

## A sane bring-up order

1. Virtual instruments only. Watch the log, confirm the cues land where the
   score says.
2. Light. Visible, harmless, and instantly shows a timing error.
3. Wind. First instrument with real latency, and the first that proves
   compensation works on hardware.
4. Shake. Cheap, low risk, and the first that genuinely needs the clock to be
   good.
5. Fog or mist, dry-run first with the machine unplugged and the relay clicking
   audibly, so you can hear the duty cycle behave before anything is emitted.
6. Motion, last, with the travel limits set far smaller than the rig's real
   range until you trust the score.
