---
status: active
branch: feat-first-real-device
---

# feat-first-real-device · one ESP32, one WLED strip, one fan

The first hardware in the project. Two devices chosen because `docs/hardware.md`
already argued for them: light and wind are the two instrument kinds that cannot
hurt anybody, and between them they exercise both halves of the system.

The light needs no Componium code on the device at all. WLED receives E1.31, the
conductor has spoken E1.31 since M4, and the whole path is already written. The
fan needs firmware we wrote, our own protocol, and a watchdog. One standard
protocol to a third party device; one private protocol to our own node.

## What was actually missing

M8 records the ESP32 firmware as done, with the honest note "firmware
uncompiled". That turned out to be the smaller half of the truth.

The 394 lines of protocol code compile clean on the first serious attempt, with
one missing `REQUIRES` entry. It was a good draft. But there was **no
`app_main`**, no Wi-Fi, no NVS and no provisioning, so the file was a node that
nothing could start and nothing could reach. `componium node` in Go had been
carrying the protocol's verification the whole time, which is why nobody
noticed.

## Provisioning, and why it is Improv

A network password belongs to the person whose network it is. Every other route
puts it somewhere else:

- `menuconfig` bakes it into the image, so the image cannot be shared and
  whoever builds it has to be told the password;
- a SoftAP captive portal serves a form over an unauthenticated origin from a
  device with no certificate;
- handing it to whoever is doing the flashing is the same problem wearing a
  friendlier face.

Improv Wi-Fi carries it down the USB cable the person is already holding, from a
browser tab on their own machine, into NVS on the chip. The flasher speaks it
natively. It is about 250 lines of C and it is the right 250 lines.

## The web installer, and the one constraint worth knowing

esp-web-tools, vendored rather than pulled from a CDN: this is the page you
reach for when the rig is half built and the wifi is the thing you are fixing.

**Web Serial only exists in a secure context.** The studio is served over plain
HTTP on a home network, so `navigator.serial` is simply undefined there and no
amount of asking will conjure it. That is not a bug to route around, it is the
browser refusing to hand a page on an unauthenticated origin a USB device. The
page detects it and gives the one line that fixes it:

    ssh -L 8722:localhost:8722 claude@claude-machine-02.home

`localhost` counts as a secure context, so the tunnel is enough and no
certificate is involved.

## The bug that could not be read from its symptom

Flashed fine, and the Wi-Fi step never appeared.

Every Improv RPC command number in the firmware was a slot out. They had been
written from memory: 0x02 was implemented as "identify" when 0x02 is *request
current state*, so the flasher asked what state the board was in, got silence,
waited ten seconds and reported no Improv support. Nothing else was wrong.
Nothing in a log, nothing on the board, nothing to see but a step in a dialog
that did not happen.

The correct numbers, read off `improv-wifi-serial-sdk` rather than recalled:

| command | value |
|---|---|
| send wifi settings | 1 |
| request current state | 2 |
| request info | 3 |
| request wifi networks | 4 |

The comment above the wrong ones said they came from the spec. That is the
whole argument for `web/src/core/improv.test.ts`: it reads the numbers out of
the firmware and out of the SDK the flasher actually uses, and compares them.
The SDK is a dev dependency for exactly this. Mutation verified against the
bug that shipped.

Two things came out of the same reading. A provisioned board owes an RPC result
as well as a state, because the flasher leaves that command pending for one and
settles it itself for anything else. And the scan command is worth answering:
an SSID typed from memory with a character wrong and a network out of range are
the same silence from the board's side, and the board has no screen to tell them
apart with.

## Two instruments on one board

The strip and the fan share an ESP32 and share nothing else. The fan takes CIP,
because a fan is a Componium instrument and nothing else speaks to it. The strip
takes sACN, because lighting already has a protocol and LOGBOOK.md lists
competing with it as a non-goal in as many words.

The result is that a rig entry for this strip *is* a WLED entry with a different
address in it. The board is a drop in substitute for a WLED controller, which is
what makes it useful for testing and what makes a one board installation a real
option rather than a compromise.

One colour across the whole length, which is what the conductor sends: a
fixture's three channels, not a pixel array. An ambient wash is a colour, not a
picture.

## Editing the rig, and what nearly got deleted

The page sends the handful of fields it can edit. An instrument also carries
things it cannot: a scent rack, a platform's declared travel, a CIP secret.
Rebuilding each one from the wire alone would have deleted all three the first
time somebody changed an address, and nothing would have announced it. So a
save merges onto what is already there, keyed by id, and there is a test that
puts a five bottle scent rack and a shared secret through a save of somebody
else's address.

The menus come from `rig.DriversFor`, which is the same table the loader
dispatches on. A driver this page offers is a driver that will start, and a new
one appears here the day it exists there rather than the day somebody remembers
this file. The parity rule in LOGBOOK.md, third time.

While wiring it up: `/api/rig` had never sent `addr` or `universe`, so the
device list had been showing a dash for every address it had. The test did not
catch it because the test mocked a response shape the server does not produce.
A fixture is only worth its accuracy, and that one was invented.

## The studio drives the room

Until now the studio was an editor and a simulator: it read the rig to draw a
preview and never opened a socket to anything. The thing that drove hardware
was `componium play`, following mpv rather than the timeline you are editing.
Right for a show, useless for the half hour where the question is whether a cue
lands on the frame you put it on.

**Almost none of it was new work, and that is the finding.** `show.Run` already
turns any `source.TimeSource` into a disciplined clock with latency
compensation, a curve driver and a safety supervisor. `source.Studio` is a
forty line TimeSource fed by the page over HTTP. The studio needed an adapter
and a switch, not a second timing stack. Every timing property a show has, live
output has, because it *is* a show with a different clock on the front.

Two properties of the source carry the weight:

- **It interpolates.** The page reports at the film's rate, around 24 a second;
  the show loop asks 200 times a second. A position that only moved on a report
  would quantise every cue to a frame boundary in the wrong direction. A real
  player's position advances between polls and so does this.
- **It goes quiet.** A tab can be closed, slept, or driven into a tunnel, and
  none of those look different from the server. After a second with no word it
  stops claiming to know where playback is, which stops dispatch; after five it
  takes the whole rig safe and disarms.

Measured end to end against a real studio: sweeping the playhead from 8s to
12.5s over a demo score dispatched the gust at 10s (1.2s early, from the
instrument's declared latency), sent 203 curve updates, and held 5ms precision.
The silence timer then demonstrated itself twice by accident, disarming while a
shell was being started.

**Worth knowing before arming on the deployed stack:** the `conductor` container
is already driving the same rig from mpv. Arm the studio as well and one node
has two sources telling it what to do. Not dangerous, since the node's own
watchdog is unchanged, but confusing. For bench work, stop the conductor first.

## Decisions

- **2026-09-01 · The image is a directory on disk, not something in the
  binary.** It is a build artifact of a different toolchain on a different
  release schedule and it is the best part of a megabyte. Tying a studio
  release to a firmware release is a cost neither of them asked for. So:
  `-firmware <dir>`, exactly like `-media` and `-scores`, and absent is the
  ordinary case that the page reports rather than fails on.
- **2026-09-01 · Credentials are tested before they are stored.** Storing first
  leaves a device that boots into a network it cannot join, having forgotten
  the one it could, which on a board with no screen and no buttons is
  indistinguishable from a brick.
- **2026-09-01 · Wi-Fi power saving is off.** It parks the radio between
  beacons and adds tens of milliseconds of jitter to a datagram that has to land
  on a frame. The entire project is about that number being small.
- **2026-09-01 · The node starts even with no network.** Blocking on the wifi
  would mean a provisioned board out of range does nothing at all, including
  nothing safe. It comes up, the watchdog runs, and it answers the moment an
  address arrives.
- **2026-09-02 · Off by default, and it puts itself away.** A rig left armed by
  somebody who wandered off is a fan running all night, which is the hazard the
  node's own watchdog exists for, one level up. So arming is explicit, the page
  reports while paused as well as while playing so that silence can only mean
  gone, and closing the tab disarms on `pagehide` rather than waiting out the
  timer. The timer is the guarantee; the beacon is the courtesy.
- **2026-09-02 · The switch is on the toolbar, not in the admin.** Everything
  else about devices lives in the admin, and this does not, because armed is a
  state you must be able to see while working. It is red on purpose.
- **2026-09-02 · An all virtual rig says so.** An armed rig that moves nothing
  looks identical to a broken one from across a room, and that room is where
  somebody is about to conclude the hardware is dead.
- **2026-09-01 · The admin is a section, not more buttons on the toolbar.** The
  studio's bar is a working surface used with a film open and a hand on the
  playhead. This is where you go when something is not set up yet, which is a
  different activity at a different pace. The studio stays mounted behind it,
  because losing an undo history to look at a port number is absurd.
- **2026-09-01 · The light's watchdog is not the fan's watchdog.** CIP goes safe
  after 300ms because a fan running all night is the hazard it exists for. A
  light is not that hazard, and 300ms of ordinary network hiccup would make it
  flicker. The danger for a strip is the opposite one, sitting lit for ever
  after the conductor has gone, so it holds through a stumble and goes dark
  after five seconds of real silence.
- **2026-09-01 · The radio starts even with nothing to join.** Scanning needs
  it running, and the scan is the first thing the flasher asks for on a board
  that has never been provisioned. Connecting is gated on having a target
  instead, so an unprovisioned board does not spend its time failing to join
  the empty string.
- **2026-09-01 · Devices is read only.** The rig is a file every machine running
  the show reads. A settings page that edited it would be a second source of
  truth for the one thing in the system that must not have two.
  **Reversed the same day, and the argument was never against editing.** It was
  against a *second copy*. The admin now edits the rig file itself: same file,
  same one source of truth, a second way to reach it. Typing an IP address into
  a table beats typing it into TOML over SSH, and nothing about that requires a
  parallel store.

  What it does require is that everything survive a round trip, and two things
  did not. `omitempty` in BurntSushi's encoder does not treat a numeric zero as
  empty, so every virtual fogger was given `start = 0`, which is not a DMX
  address and which the validator then refused: the file could not be written
  by the code that wrote it. And `Duration` had `UnmarshalText` and no
  `MarshalText`, so a latency encoded as an integer of nanoseconds would not
  read back. Neither was visible until something wrote one of these files, and
  nothing ever had.

## Where this stands

| step | state |
|---|---|
| firmware compiles | **done.** Clean, no warnings, ESP-IDF v5.4. |
| entry point, Wi-Fi, NVS | **done.** |
| Improv provisioning | **fixed.** The first version was never spoken to a real flasher and it showed: every command number was a slot out. Now checked against the SDK by a test. |
| LED strip over sACN | **written, never seen a pixel.** |
| web installer page | **done**, served from the studio. |
| admin shell | **done.** Navbar, side menu, three pages. |
| flashed to a board | **done**, and it taught us the command numbers. |
| WLED over sACN | **never tested against a fixture.** M4 has said so since M4. |
| measured fan latency | not yet, and the firmware's 1.2s is a guess. |

The last three are the whole point and none of them can be done from here. The
numbers in `examples/bench-rig.toml` are placeholders in the strict sense: they
are what somebody guessed, and the first real job is to replace them with what
somebody measured.
