# Running the demonstration

```sh
docker compose -f deploy/docker-compose.yml up -d
```

Then open `http://<host>:8722`.

No hardware. Five containers from one image:

| | |
|---|---|
| `clips` | Generates the test clip once, then exits |
| `scores` | Seeds an editable copy of the demo score, then exits |
| `player` | mpv, headless, looping the clip, exposing its IPC socket |
| `node` | A software instrument. What the ESP32 firmware does, minus the pins |
| `conductor` | Follows the player and drives the node and a virtual light |
| `studio` | The timeline editor. This is the part with a URL |

## What to look at

```sh
docker compose -f deploy/docker-compose.yml logs -f conductor
```

Cues are dispatched **early by each instrument's declared latency**, which for
the fan is 1.2 seconds:

```
CUE  wind.main  gust  cue at 30s  sent at 28.802s  1.198s early
CUE  wind.main  stop  cue at 32s  sent at 30.802s  1.198s early
```

Both ends of the span are compensated, so the effect both begins and ends where
the score says.

```sh
docker compose -f deploy/docker-compose.yml logs -f node
```

The node reports what its output is actually set to, and returns to `SAFE` when
a span's duration expires, whether or not the stop arrives.

The studio edits the same score the conductor is playing. Change a cue time,
save, and the next pass through the film uses it.

## Why the socket is on a volume

The conductor reaches mpv through a unix socket rather than a network port,
because mpv's IPC is a socket and putting it on a shared volume is simpler and
faster than bridging it. It also means neither container needs to know the
other's address.

## Pinning a version

`COMPONIUM_IMAGE` overrides the image. CI publishes `:main` and a tag for every
commit:

```sh
COMPONIUM_IMAGE=ghcr.io/slicit/componium:<sha> docker compose -f deploy/docker-compose.yml up -d
```

Building locally instead:

```sh
docker build -t componium:local .
COMPONIUM_IMAGE=componium:local docker compose -f deploy/docker-compose.yml up -d
```
