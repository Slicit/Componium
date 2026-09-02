# Firmware

A Componium instrument node for the ESP32, speaking CIP over UDP and driving a
PWM output. Enough for a fan, a dimmable light, a mister, or anything else that
takes a level between 0 and 1.

## Status: compiles, never run on a device

Built clean against ESP-IDF v5.4. It has still never been on an ESP32, so read
`componium_node.c` rather than trusting it, and expect the first flash to teach
you something.

What the earlier draft was missing turned out to be larger than the note
admitted. The protocol code was sound and needed one missing `REQUIRES` entry,
but there was no `app_main`, no Wi-Fi, no storage and no provisioning: a node
nothing could start and nothing could reach. Those are written now.

The protocol itself *is* verified, because `componium node` implements the same
protocol in Go and is tested end to end against the real client.

## What is attached

The board holds a configuration in its own storage saying which device is on
which pin, reads it at boot, and announces the result. Three types, which is
what an ESP32 usefully drives:

| type | for | carries |
|---|---|---|
| `pwm` | fans, dimmable lights, misters | `gpio`, `freq_hz` |
| `ws28xx` | addressable strips | `gpio`, `pixels` |
| `relay` | foggers, valves, anything switched | `gpio`, `active` |

Each also carries the physical facts about the thing on the end of the wire:
`kind`, `latency_ms`, `ramp_up_ms`, `ramp_down_ms`, `safe`.

That last part is the point of the whole arrangement. The conductor fires every
cue `latency_ms` early, and until now that number was a `#define`: measuring a
fan's real dead time meant editing C and reflashing, so nobody did, and the
shipped guess stayed a guess for the life of the rig. **Measure yours and put
the real number in the configuration.**

A board with no configuration announces nothing and waits. That is an ordinary
state, and the one every freshly flashed board is in.

## Which pins

Not all of them, and the last row is the one to be careful with.

| pins | why not |
|---|---|
| 34 to 39 | Input only. From the chip's own `SOC_GPIO_VALID_OUTPUT_GPIO_MASK`. |
| 6 to 11 | The SPI flash. The chip calls them valid; using one stops the board running. |
| 1, 3 | The console UART, where provisioning lives. Taking it removes the way back in. |
| 0, 2, 12, 15 | Strapping pins, read at boot. 12 held high can leave a board that will not start. |

And there is a limit on how many, not only which: **8 RMT channels** so at most
eight strips, and **8 PWM channels across 4 timers** so at most eight dimmed
outputs sharing one frequency. Three or four devices on a board is comfortable.

A configuration naming a pin from that table is refused whole, with the reason,
rather than applied in part.

## Wi-Fi

There are no credentials in this repository and none in a build.

**The shared secret is required, not optional.** This board accepts
configuration, and a stranger who can write one can move a relay onto a pin
nobody intended or declare a latency of zero, which corrupts the timing of every
cue after it in a way that reads as the score being wrong rather than as an
attack. So the requirement follows from the capability: a node that takes
configuration ignores every unauthenticated datagram, including `hello`.

The secret is written over USB with the wifi credentials. **There is no recovery
path over the network, deliberately.** Losing it means reconnecting USB and
reflashing, because a remote way back in is a way in, and the entire security
model here is that the board ignores anyone who cannot prove they know the key.
 The board is
told its network over the USB cable, by the person holding it, using
[Improv](https://improv-wifi.com) from the studio's admin page. They land in
NVS on the chip and are tested before they are stored: storing first leaves a
board that has forgotten the network it could join and boots into one it
cannot, which on a device with no screen is indistinguishable from a brick.

## Building

```sh
. $IDF_PATH/export.sh
cd firmware/esp32
idf.py set-target esp32
idf.py build
./make-web-install.sh          # writes web/, for the browser flasher
```

Then serve it from the studio and flash from a browser, which needs no
toolchain on the machine the board is plugged into:

```sh
componium studio -firmware firmware/esp32/web ...
```

Open Admin, Firmware. Web Serial needs a secure context, so if the studio is on
another machine, tunnel it with `ssh -L 8722:localhost:8722 <host>` and open
`http://localhost:8722`. Chrome or Edge; Firefox and Safari have no Web Serial
at all.

`idf.py flash monitor` still works if you would rather.

## The one rule

The watchdog is not optional. If heartbeats stop arriving for 300 ms the output
goes to its safe value, without asking anyone and without depending on the
network being healthy or the conductor being correct. That is the only thing
standing between a crashed conductor and a fan running all night.

Anyone porting this to another board should port that first and the PWM second.

## Why not ESPHome

ESPHome's ergonomics are excellent and worth borrowing: declarative
configuration, OTA, discovery. Its control path is not. It talks to Home
Assistant at 100 to 300 ms with non-deterministic jitter, which is fine for
turning on a lamp and useless for landing a cue on a frame. See
`docs/adr/0002-esp32-node.md`.

## No hardware?

`componium node` is the same node in Go, and a complete rig can be run over the
network with no microcontroller at all:

```sh
componium node -id wind.main -addr 0.0.0.0:5570
componium play -score examples/demo.componium -rig examples/remote-rig.toml
```
