---
status: shipped
branch: feat-analysis-engine
parent: feat-composer-v1
---

# Analysis engine · what the film is doing

## Intent

Turn scenes into events, with four things the earlier composer could not do:
recognise calm, estimate camera movement and speed, spot a plunge, and drive
light in two layers rather than one.

## Calm is the feature, not a side effect

The most common failure of an automatically scored film is not a missed effect.
It is that everything is scored, all the time, and after twenty minutes nobody
notices any of it. Being shaken continuously for two hours is not immersive, it
is tiring.

So `dynamics` decides what *not* to play, by two separate mechanisms:

- **Calm regions are protected.** Stretches the film itself keeps quiet stay
  quiet. Silence is something the score contains, not an absence of score. A
  loud event still interrupts calm, because a thunderclap in a silent scene is
  the whole reason the scene was silent.
- **A rest budget caps density everywhere else.** Only a quarter of any two
  minute window may be spent doing something. When the cap bites, the
  *quietest* cue in the window is dropped rather than the newest, so what
  survives is the peaks.

## Decisions

### 2026-08-29

- **Decision:** Motion is estimated by projection matching on a 64x36 frame,
  not by optical flow.
- **Why:** A camera pan shifts every column by the same amount, so the sum of
  each column shifts too, and finding that shift is a search over 64 numbers
  rather than 2304 pixels. Downscaling is also a very good low pass filter: it
  removes exactly the detail that makes frame matching noisy. It runs in pure
  Python at roughly a minute per hour of film, with no dependencies.

### 2026-08-29

- **Decision:** A sustained vertical movement is a *plunge candidate*, not a
  plunge.
- **Why:** A camera tilting down and a camera falling are identical through a
  lens. Nothing in the image distinguishes them, so the honest output is a
  nomination for something that understands the scene to confirm.

### 2026-08-29

- **Decision:** Water is nominated by colour and position, never acted on
  alone.
- **Why:** A blue lower frame is a sea, a rain soaked street, a swimming pool,
  and also a blue lit office and a night interior. Driving a mister from an
  unconfirmed nomination is how somebody's sofa gets wet during a dialogue
  scene. A nomination becomes a cue only when a subtitle or the vision model
  agrees.

### 2026-08-29

- **Decision:** Ambient light is two layers on two instruments, and the soft
  one is ceilinged at 0.65.
- **Why:** One track cannot do both jobs. Follow the picture closely enough to
  feel like ambient light and there is no headroom for lightning; scale for
  lightning and the ambient half sits at a tenth of brightness and looks
  broken. The ceiling is what makes a spike read as a spike.

### 2026-08-29

- **Decision:** Flash detection runs at the film's own frame rate, everything
  else at 4 Hz.
- **Why:** Found by measurement. A lightning strike lasts about 150 ms, and at
  4 Hz four out of five fall between samples: the fixture has five flashes and
  the first pass found four. A one pixel grayscale pass is one byte per frame,
  so 24 Hz over a two hour film costs 173 kilobytes.

## Two bugs the fixture found

The old test clip is a scrolling pattern with a constant tone: uniformly loud,
uniformly busy, permanently moving. It cannot answer any question about what a
film is doing. `hack/make-dynamics-clip.sh` builds one with structure instead,
25 s of calm, five flashes, a fast fall, 20 s of calm, so every detector has a
known answer to be wrong about.

It immediately found two things unit tests had not:

**A static dark shot was the fastest movement in the film.** On a featureless
frame every candidate shift scores identically, and `min()` returns the first
key, which is `-max_shift`. So a blank frame reported maximum movement, at full
weight, in every calm scene. Ties now break toward zero, and a match with no
confidence reports no movement rather than whatever the search landed on.

**A quiet stretch scored as maximally busy.** `normalise` divides by the peak,
so a uniformly quiet signal comes back uniformly *maximum*. The audio envelope
was already peak normalised upstream and `activity` normalised it again, which
turned near silence into 1.0. Calm detection found zero regions on a fixture
that is more than half calm.

## Verification

Forty four tests over the engine, and end to end against the structured
fixture, where the expected answer is known:

```
fixture : calm 0-20 and 55-75, five flashes 20-35, one plunge 35-55
calm    : (0.0, 22.8), (55.5, 75.5)
plunges : one run, 37.0 to 55.0
flashes : five
```

## Known limitations

- A plunge is emitted as a span holding a constant heave, so an eighteen second
  fall is eighteen seconds of sitting lower rather than the shape of a fall.
  Expressing that needs a curve track, which the format supports and this does
  not yet use.
- Everything was measured on synthetic fixtures. No real film has been scored,
  and the thresholds are reasoned rather than tuned.
- Speed is apparent image movement, which is camera movement plus whatever
  large thing is moving in front of it. There is no way to separate them here.

## Links

- Branch: `feat-analysis-engine`
- Related features: `feat-composer-v1`, `feat-spans`
