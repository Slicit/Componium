# Firmware

A Componium instrument node for the ESP32, speaking CIP over UDP and driving a
PWM output. Enough for a fan, a dimmable light, a mister, or anything else that
takes a level between 0 and 1.

## Status: written, not compiled

No ESP32 and no ESP-IDF toolchain were available while this was written. It is
a careful draft, not working firmware, and it has never run on a device. The
protocol it implements *is* verified, because `componium node` implements the
same protocol in Go and is tested end to end against the real client.

Anyone with the hardware should expect to fix compilation errors before
expecting it to work, and should read `componium_node.c` rather than trusting
it.

## Building, once you have the toolchain

```sh
. $IDF_PATH/export.sh
cd firmware/esp32
idf.py set-target esp32
idf.py build flash monitor
```

You will need to add Wi-Fi provisioning; this file deliberately contains no
credentials and no network setup, because those belong to whoever owns the
device.

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
