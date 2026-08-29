# Composer

Generates a Componium score from a film. Offline, slow by design, and never
part of the realtime path: it consumes media and emits a score file, and the
conductor knows nothing about how that file was made.

```sh
python3 compose.py film.mkv -o film.componium --media-fps 24
componium validate -score film.componium -rig my-rig.toml
```

No dependencies beyond ffmpeg and Python 3. That is deliberate: the composer
needs to run wherever ffmpeg does.

## What v0 extracts

**LFE energy to shake.** The audio is low passed at 120 Hz, reduced to mono at
1 kHz, and turned into an RMS envelope. Working at 1 kHz rather than 48 kHz
makes this cheap enough to run over a feature film without anyone noticing.
Sub bass maps almost directly onto rumble: explosions, engines and thunder all
live there.

**Average frame colour to ambient light.** One ffmpeg filter chain scales each
sampled frame to a single pixel, so ffmpeg does the averaging. This is what
Ambilight does, and it demonstrates the whole pipeline end to end.

Both are deliberately dumb. The expensive signals come later.

## Curve compression

A two hour film sampled four times a second is 28,800 points per track, most of
which repeat their neighbour. Points within `--threshold` of the last kept one
are dropped, which typically removes an order of magnitude and makes the score
something a person can open and edit. The first and last points are always
kept, so the curve still spans the film.

Measured on a two minute clip: 480 samples became 87 light points.

## The output is a proposal

Every generated score carries a header saying so. Nothing in it has been
checked against what a particular rig can survive, which is why the intended
workflow is generate, review in the studio, then play. That review step is a
safety control, not a nicety.

## Testing

```sh
python3 -m unittest test_compose
```

Extraction needs ffmpeg and a real file, so it is exercised by running the
composer against a clip. Everything that turns signals into a score is pure and
is unit tested.
