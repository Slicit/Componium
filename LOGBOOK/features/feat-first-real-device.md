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
- **2026-09-01 · The admin is a section, not more buttons on the toolbar.** The
  studio's bar is a working surface used with a film open and a hand on the
  playhead. This is where you go when something is not set up yet, which is a
  different activity at a different pace. The studio stays mounted behind it,
  because losing an undo history to look at a port number is absurd.
- **2026-09-01 · Devices is read only.** The rig is a file every machine running
  the show reads. A settings page that edited it would be a second source of
  truth for the one thing in the system that must not have two.

## Where this stands

| step | state |
|---|---|
| firmware compiles | **done.** Clean, no warnings, ESP-IDF v5.4. |
| entry point, Wi-Fi, NVS | **done.** |
| Improv provisioning | **written, never spoken to a real flasher.** |
| web installer page | **done**, served from the studio. |
| admin shell | **done.** Navbar, side menu, three pages. |
| flashed to a board | not yet. Needs the hardware in hand. |
| WLED over sACN | **never tested against a fixture.** M4 has said so since M4. |
| measured fan latency | not yet, and the firmware's 1.2s is a guess. |

The last three are the whole point and none of them can be done from here. The
numbers in `examples/bench-rig.toml` are placeholders in the strict sense: they
are what somebody guessed, and the first real job is to replace them with what
somebody measured.
