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

## Wi-Fi

There are no credentials in this repository and none in a build. The board is
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
