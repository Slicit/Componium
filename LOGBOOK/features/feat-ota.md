---
status: active
branch: feat-ota
---

# feat-ota · updating a board without a ladder

A node is a small box on a wall, behind a screen, or in a ceiling. Every
firmware change so far has meant a USB cable, which means reaching the box, and
reaching the box is the expensive part of every other decision in this project.
Worse, flashing over USB used to erase the whole chip, so a board came back
having forgotten its network and its wiring, and both had to be typed in again
through a serial console.

Two separate problems, and the second one had to go first.

## The layout, which was the real blocker

The stock single app partition table puts NVS at `0x9000` with `0x4000` of
room. Two app slots need somewhere for `otadata`, and the obvious place is the
gap NVS was using. Taking it would have meant every board losing its settings
on the update that introduced updates, which is the worst possible first
impression for the feature.

So `firmware/esp32/partitions.csv` keeps NVS where it already was and gives it
`0x6000`, and puts `otadata` after it at `0xf000`. The app slots are `0x20000`
and `0x1e0000`, 1.8MB each, on the 4MB parts this project uses.

`make-web-install.sh` writes four separate parts rather than one blob at offset
zero, so a USB flash lands on the bootloader, the partition table, `otadata`
and the app, and nothing is written across `0x9000` to `0xf000`. A board
flashed over USB now keeps its wifi credentials and its device configuration,
which is worth having on its own and is not really an OTA feature at all.

## Authenticating an instruction is not authenticating an image

The update is a CIP message, signed and counted like every other control
message. That says the instruction came from somebody holding the secret, and
it is not enough. The instruction names a URL, and whatever answers that URL is
authenticated by nothing whatsoever. A board that trusted it would run whatever
the network handed back, on a device that switches mains.

So the message carries an HMAC of the image over the same secret, and the board
checks it against what actually arrived, before the image is made bootable. The
image is not secret and does not need to be, which is why it can be fetched
over plain HTTP from wherever is convenient. It needs to be provably the one
that was meant, and that is what an HMAC says. An update with no MAC is
refused: this is the one message that replaces the code checking every other
message, so it gets no lenient path.

Same secret as CIP, deliberately, because it answers the same question. Which
is also why the update goes over CIP and not the board's web page: Basic auth
sends the secret in clear on every visit, and sniffing a page login should not
be enough to push firmware.

## Arranged around going wrong

- The image is written to the slot the board is not running from, so a download
  that stops halfway leaves a working board.
- The boot partition moves only once the whole image has been verified.
- The new image boots on probation. It calls
  `esp_ota_mark_app_valid_cancel_rollback()` only after it has an address and
  somebody has authenticated to it, which are the two things a bad image fails
  at. An image with the wrong secret or a broken network undoes itself at the
  next power cycle instead of needing the ladder.

## The button

The studio is the only thing holding both the image and the secret, so it is
the thing that can do this. Pressing update on a board makes the studio pick
the application out of the package, sign it with that board's secret, work out
an address, and send the instruction. Nothing passes through the browser.

Two of the ways that could go wrong end with somebody on a ladder, and both are
tested:

- **Sending the wrong part.** The package has four parts and the bootloader is
  first. An update replaces the application and nothing under it, so the studio
  picks the part at the highest offset rather than the first one or one matched
  by name. A layout change still needs USB, and the page says so.
- **Sending an unreachable address.** The address is asked of the routing
  table, by opening a socket towards the board and reading which of this
  machine's addresses the kernel chose. Not asked of the browser, which is very
  often reaching the studio through an ssh tunnel at localhost: telling a board
  on a shelf to fetch from localhost is telling it to fetch from itself. A
  studio that can only reach a board over loopback refuses rather than sending
  an address that means something only here.

The port in that URL is the one the studio was told to listen on, not the one
the request arrived on, for the same tunnel reason.

## What is tested, and what is not

Three tests against real firmware on an emulated board, in
`internal/emulated/update_test.go`. An update with no signature is refused. An
update whose image does not match its signature is refused after downloading
it, and the board carries on running what it had. And the whole thing end to
end: fetched, verified, written, rebooted, answering again, moved from `0x20000`
to `0x1e0000`.

That last one is opt in, behind `COMPONIUM_EMULATED_OTA`. The update works; the
restart under emulation does not always. About one run in three the board dies
inside ESP-IDF's own startup at `esp_intr_alloc`, and does not always come back
inside two minutes. Nothing of ours is in that backtrace, a power on reset
never does it, and it gets likelier the longer the board has been up, which
reads as QEMU not resetting state that real silicon resets. A suite that fails
one time in three teaches people to ignore it, and that costs more than the
test earns. The two refusal tests run every time and they are the ones guarding
the dangerous half.

Nine tests in `internal/studio/otaapi_test.go` cover the studio side.

Not confirmed on hardware. It cannot be until a board has been flashed over USB
once more, because the OTA capable layout is itself a layout change. That flash
is the last cable this should ever need.

## Open

- The Go software node does not answer an `update` message at all. Nothing
  depends on it yet, but it is a parity gap: a virtual instrument should be
  able to stand in for a real one everywhere the real one is addressed.
- A board that is offline cannot be told anything, so an update is a thing you
  do to boards that are working. There is no queue and there should probably
  not be one, but it is worth writing down that this was a choice.
