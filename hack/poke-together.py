"""Drive every output on one board at once, over authenticated CIP 0.3.

The companion to poke.py, which speaks 0.2 unauthenticated and drives one
output at a time. That was the right tool when a node was an instrument. A node
now carries several, so the interesting question moved: not "does the fan
spin", which poke.py already answers, but "do the fan and both strips move
together, and does each strip get the colour meant for it".

    python3 poke-together.py 192.168.1.75 <secret>            # everything
    python3 poke-together.py 192.168.1.75 <secret> hello      # just look
    python3 poke-together.py 192.168.1.75 <secret> rollcall   # one movement

Colour is the reason this is worth doing by eye rather than by counter. Two
strips both lit is not evidence: two strips lit in *different* colours, then
swapping colours in step, is. Almost every way of getting the addressing wrong
ends with both strips showing the same thing, and the counters cannot see that
because from the board's side the frame was applied perfectly.

Five movements, each failing differently on purpose:

  rollcall   Each light alone in its own colour, everything else dark. This is
             the one that maps an announced index onto a strip you can point
             at. Run it first on a board you have just wired.
  contrast   Every light at once, each a different colour, held still. Side by
             side, which is where a colour that is nearly right looks wrong.
  swap       The colours rotate between the strips, all in one datagram. Two
             strips changing at visibly different moments means the bundle is
             not doing its job.
  bundle     Fifty frames a second: the fan ramps while every strip sweeps the
             hue circle, each one a different distance around it, so no two are
             ever the same colour.
  watchdog   The heartbeats stop. Everything should go dark and go still
             within 300ms, without being told to.

The counters come off the board's own status page afterwards, so the report is
what the board did rather than what this sent.

Two things to know before trusting your eyes. A strip that names the wrong
primary is not a plain WS2812, and the answer is that strip's `order` in the
studio rather than anything in this file: the board honours it as of
firmware v0.1.0-alpha.1-98, and ignored it in every build before that, so an
older board will keep showing the wrong colour however it is configured.
And a node keeps a single replay counter for the whole board rather than one
per sender, so anything else talking to it during a run, the studio's Boards
page included, silences this script for the rest of the run. See
LOGBOOK/notes.md.
"""

import hashlib
import hmac
import json
import re
import socket
import struct
import sys
import time
import urllib.request

CIP_PORT = 5570
CIP_VERSION = "0.3"
TAG_LEN = 16
WATCHDOG_MS = 300

# Named because the operator is going to read one of these off the terminal and
# compare it against something glowing on a bench. Ordered most distinguishable
# first, so a board with two strips gets red against green rather than amber
# against magenta.
PALETTE = [
    ("red", (1.0, 0.0, 0.0)),
    ("green", (0.0, 1.0, 0.0)),
    ("blue", (0.0, 0.0, 1.0)),
    ("amber", (1.0, 0.45, 0.0)),
    ("magenta", (1.0, 0.0, 1.0)),
    ("cyan", (0.0, 1.0, 1.0)),
    ("white", (1.0, 1.0, 1.0)),
]
DARK = (0.0, 0.0, 0.0)


class Link:
    """One authenticated conversation with a node.

    The counter is seeded from the clock in microseconds, exactly as the Go
    client does, and for the same two reasons. It has to beat whatever the node
    last heard, which a counter starting at 1 does not after the first client
    of the boot. And it has to stay under 2^53, because the other end parses it
    into a double: nanoseconds do not, and two consecutive nanosecond counters
    land on the same float and the second message is refused as a replay.
    """

    def __init__(self, host, secret):
        self.host = host
        self.secret = secret.encode()
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.n = int(time.time() * 1e6)
        self.sent_frames = 0
        self.sent_outputs = 0
        self.sent_cues = 0

    def wrap(self, body):
        if not self.secret:
            return body
        tag = hmac.new(self.secret, body, hashlib.sha256).digest()[:TAG_LEN]
        return tag + body

    def unwrap(self, datagram):
        """Verify and strip the tag on the way in.

        The node signs what it sends as well as what it accepts, which is easy
        to forget when writing a client: a reply read without stripping the tag
        is sixteen bytes of binary followed by valid JSON, and json.loads says
        only that it is not JSON. That looked exactly like a board not
        answering, for about ten minutes.
        """
        if not self.secret:
            return datagram
        if len(datagram) < TAG_LEN:
            return None
        body = datagram[TAG_LEN:]
        want = hmac.new(self.secret, body, hashlib.sha256).digest()[:TAG_LEN]
        if not hmac.compare_digest(want, datagram[:TAG_LEN]):
            return None
        return body

    def send(self, message):
        self.n += 1
        message = dict(message, v=CIP_VERSION, n=self.n)
        self.sock.sendto(self.wrap(json.dumps(message).encode()), (self.host, CIP_PORT))

    def send_frame(self, outputs):
        """One curve frame carrying several outputs.

        Binary, and not counted: a frame is superseded 20ms later, so the
        replay guard would cost more than it protects. The tag still applies.
        """
        body = bytearray([ord("C"), ord("F"), 1, len(outputs)])
        for index, values in outputs:
            body.append(index)
            body.append(len(values))
            for v in values:
                body += struct.pack(">f", max(0.0, min(1.0, v)))
        self.sock.sendto(self.wrap(bytes(body)), (self.host, CIP_PORT))
        self.sent_frames += 1
        self.sent_outputs += len(outputs)

    def cue(self, instrument, params, hold_ms=8000):
        self.sent_cues += 1
        self.send({"t": "cue", "seq": self.sent_cues, "instrument": instrument,
                   "params": params, "hold_ms": hold_ms})

    def ask(self, message, wait=2.0):
        self.send(message)
        self.sock.settimeout(wait)
        deadline = time.time() + wait
        while time.time() < deadline:
            try:
                data, _ = self.sock.recvfrom(4096)
            except socket.timeout:
                return None
            body = self.unwrap(data)
            if body is None:
                continue        # not signed with our secret, so not for us
            try:
                return json.loads(body.decode())
            except (UnicodeDecodeError, ValueError):
                continue        # a curve frame, or somebody else's datagram
        return None

    def beat(self):
        self.send({"t": "heartbeat"})

    def close(self):
        self.sock.close()


def hold(link, seconds, paint=None):
    """Keep the outputs alive for a while, beating as the conductor would.

    The heartbeats are the point of this helper. Without them the node drops
    everything to safe after 300ms, which looks exactly like a cue that never
    landed. `paint` is called with elapsed seconds when a movement wants to
    keep sending frames while it waits.
    """
    start = time.time()
    last_beat = -1.0
    while True:
        t = time.time() - start
        if t >= seconds:
            return
        if t - last_beat > 0.1:
            link.beat()
            last_beat = t
        if paint:
            paint(t)
        time.sleep(0.02)


# --- what is attached ------------------------------------------------------

def announced(link):
    reply = link.ask({"t": "hello"})
    if reply is None:
        return None
    return reply.get("instruments") or []


def channels_of(inst):
    return len(inst.get("channels") or [])


def sort_out(instruments):
    """Split what is attached into things with a colour and things without.

    By channel count rather than by id, because the ids belong to whoever set
    the board up. Three channels is a colour whatever it is called, and one
    channel is a level. Kind is used only to order the lights, so that adding a
    strip does not renumber the colours of the ones already on the bench.
    """
    lights = [i for i in instruments if channels_of(i) == 3]
    levels = [i for i in instruments if channels_of(i) == 1]
    lights.sort(key=lambda i: i.get("index", 0))
    levels.sort(key=lambda i: i.get("index", 0))
    return levels, lights


def describe(instruments):
    levels, lights = sort_out(instruments)
    for i in instruments:
        colour = ""
        if i in lights:
            colour = "  will show %s" % PALETTE[lights.index(i) % len(PALETTE)][0]
        print("  %d  %-16s %-7s %-7s gpio %-3s %d channel(s)%s" % (
            i.get("index", -1), i.get("id", "?"), i.get("kind", "?"),
            i.get("type", "?"), i.get("gpio", "?"), channels_of(i), colour))
    if len(lights) < 2:
        print()
        print("  only %d light attached, so nothing here can tell whether two" % len(lights))
        print("  strips are addressed separately. Attach the second and rerun.")


# --- the board's own account of it -----------------------------------------

def counters(host, secret):
    """Cues applied, curve outputs driven and datagrams refused, per the board.

    Read off the status page on port 80, which is the only place the node
    reports them. Scraped, because the page is for a person and this is the one
    machine reading it; if that ever matters, hello should carry them.
    """
    url = "http://%s/" % host
    manager = urllib.request.HTTPPasswordMgrWithDefaultRealm()
    manager.add_password(None, url, "", secret)
    opener = urllib.request.build_opener(
        urllib.request.HTTPBasicAuthHandler(manager))
    try:
        with opener.open(url, timeout=5) as answer:
            page = answer.read().decode("utf-8", "replace")
    except Exception as why:                      # noqa: BLE001
        print("  (no status page: %s)" % why)
        return None

    found = re.search(
        r"<dd>(\d+) applied, (\d+) curve frames</dd>.*?<dd>(\d+) datagrams</dd>",
        page, re.S)
    if not found:
        return None
    return {"cues": int(found.group(1)), "outputs": int(found.group(2)),
            "refused": int(found.group(3))}


# --- painting --------------------------------------------------------------

def frame_for(levels, lights, level_value, colours):
    """One bundle: every output on the board, in the order it was announced."""
    outputs = []
    for d in levels:
        outputs.append((d["index"], [level_value]))
    for d, rgb in zip(lights, colours):
        outputs.append((d["index"], list(rgb)))
    return outputs


def hue(turns):
    """A point on the hue circle as rgb in 0..1, at full saturation."""
    h = (turns % 1.0) * 6.0
    c = 1.0
    x = c * (1 - abs(h % 2 - 1))
    table = [(c, x, 0), (x, c, 0), (0, c, x), (0, x, c), (x, 0, c), (c, 0, x)]
    return list(table[int(h) % 6])


# --- the movements ---------------------------------------------------------

def movement_rollcall(link, levels, lights, seconds=2.0):
    """Each light alone, in its own colour, everything else dark.

    The movement that turns an index into a strip you can point at. Everything
    else in this file assumes that mapping is right; this is the only part that
    establishes it, which is why it runs first and why it says out loud what
    should be lit before it lights it.
    """
    print("rollcall, one light at a time")
    if not lights:
        print("  no lights attached")
        return
    for k, d in enumerate(lights):
        name, rgb = PALETTE[k % len(PALETTE)]
        print("  index %d  %-16s should be the ONLY one lit, and %s"
              % (d["index"], d["id"], name), flush=True)
        colours = [DARK] * len(lights)
        colours[k] = rgb
        hold(link, seconds,
             lambda t, c=colours: link.send_frame(frame_for(levels, lights, 0.0, c)))
    print("  all dark")
    hold(link, 0.4, lambda t: link.send_frame(
        frame_for(levels, lights, 0.0, [DARK] * len(lights))))


def movement_names(link, levels, lights, seconds=3.0):
    """Rollcall again, addressed by id instead of by index.

    The two are different paths and only one of them has ever been checked
    against a bench. A curve frame carries a position in the announcement, and
    the board indexes an array with it. A cue carries an id, and the board
    walks that array comparing strings. A score drives lights with cues, so if
    those two ever disagree, the movement that looks right is the one nothing
    plays.

    Same colours as rollcall, deliberately. Run them back to back: any strip
    that lights in one and not the other is the bug, and the strip it lights
    instead is the evidence.
    """
    print("names, one light at a time, addressed by id")
    if not lights:
        print("  no lights attached")
        return
    for k, d in enumerate(lights):
        name, rgb = PALETTE[k % len(PALETTE)]
        print("  id %-16s should be the ONLY one lit, and %s" % (d["id"], name),
              flush=True)
        # Everything else told to go dark first, by id as well, so that what is
        # lit is what this cue lit rather than what the last one left behind.
        for other in lights:
            if other is not d:
                link.cue(other["id"], {"r": 0.0, "g": 0.0, "b": 0.0},
                         hold_ms=int(seconds * 1000) + 1000)
        link.cue(d["id"], {"r": rgb[0], "g": rgb[1], "b": rgb[2]},
                 hold_ms=int(seconds * 1000) + 1000)
        hold(link, seconds)
    for d in lights:
        link.cue(d["id"], {"r": 0.0, "g": 0.0, "b": 0.0}, hold_ms=1000)
    print("  all dark")


def movement_channels(link, levels, lights, seconds=2.0):
    """Pure red, then pure green, then pure blue, on each strip in turn.

    Because "the colours are off" and "the wrong strip lit" look identical from
    across a room and have nothing to do with each other. This says which.

    A strip that shows green when told red is wired in a different channel
    order, which the board announces and then ignores: device_apply writes r,
    g, b whatever the configuration says. What is seen here is the permutation
    somebody has to put into device_apply.
    """
    print("channels, primaries one at a time")
    if not lights:
        print("  no lights attached")
        return
    for d in lights:
        for name, rgb in (("RED", (1.0, 0.0, 0.0)), ("GREEN", (0.0, 1.0, 0.0)),
                          ("BLUE", (0.0, 0.0, 1.0))):
            print("  %-16s should be %s" % (d["id"], name), flush=True)
            colours = [DARK] * len(lights)
            colours[lights.index(d)] = rgb
            hold(link, seconds,
                 lambda t, c=colours: link.send_frame(frame_for(levels, lights, 0.0, c)))
    hold(link, 0.4, lambda t: link.send_frame(
        frame_for(levels, lights, 0.0, [DARK] * len(lights))))
    print("  all dark. A strip that named the wrong primary is wired in a")
    print("  different channel order. Set that strip's order in the studio:")
    print("  the string says which of r, g and b feeds the driver's red,")
    print("  green and blue, so a strip that showed GREEN when told RED")
    print("  wants grb. Then run this again; it should name them correctly.")
    for d in lights:
        if not d.get("order"):
            print("  (%s has no order set, so it was driven straight through)"
                  % d["id"])


def movement_contrast(link, levels, lights, seconds=4.0):
    """Every light at once, each a different colour, held still.

    Sent as cues rather than frames, because a cue is the path a score takes
    and it should reach the same place. Held still because a colour that is
    nearly right is obvious beside another one and invisible on its own.
    """
    print("contrast, every light at once, held")
    if not lights:
        print("  no lights attached")
        return
    for k, d in enumerate(lights):
        name, rgb = PALETTE[k % len(PALETTE)]
        print("  %-16s %s" % (d["id"], name), flush=True)
        link.cue(d["id"], {"r": rgb[0], "g": rgb[1], "b": rgb[2]},
                 hold_ms=int(seconds * 1000) + 1000)
    for d in levels:
        link.cue(d["id"], {"intensity": 0.5}, hold_ms=int(seconds * 1000) + 1000)
    print("  no two should look alike, and none should be dark")
    hold(link, seconds)


def movement_swap(link, levels, lights, rounds=4, dwell=0.8):
    """The colours rotate between the strips, all in one datagram.

    Two things at once. Every strip takes every colour in turn, so a strip that
    is not really being addressed separately shows it immediately by never
    changing or by changing in lockstep with its neighbour. And each change
    lands in a single frame, so the strips should switch together: one visibly
    trailing the other means the bundle is not being applied as a unit.
    """
    print("swap, colours rotating between the strips")
    if len(lights) < 2:
        print("  needs two lights to mean anything; skipping")
        return
    names = [PALETTE[k % len(PALETTE)][0] for k in range(len(lights))]
    for turn in range(rounds):
        order = [(k + turn) % len(lights) for k in range(len(lights))]
        colours = [PALETTE[o % len(PALETTE)][1] for o in order]
        print("  " + ",  ".join(
            "%s %s" % (d["id"], names[o]) for d, o in zip(lights, order)), flush=True)
        hold(link, dwell,
             lambda t, c=colours: link.send_frame(frame_for(levels, lights, 0.3, c)))
    print("  every strip should have shown every colour, and changed in step")


def movement_bundle(link, levels, lights, seconds=8.0):
    """Fifty frames a second, everything moving, nothing sharing a colour.

    Each light sweeps the hue circle from a different starting point, spaced
    evenly around it, so at every instant they are as far apart in colour as
    they can be. The fan ramps up and back down underneath, so the run also
    shows a level and a colour moving in one datagram.
    """
    print("bundle, 50Hz for %.0fs, hue sweep and a ramp" % seconds)
    spread = 1.0 / max(1, len(lights))
    start = time.time()
    last_beat = -1.0
    while True:
        t = time.time() - start
        if t >= seconds:
            break
        phase = t / seconds
        level = 2 * phase if phase < 0.5 else 2 * (1 - phase)
        colours = [hue(t / 3.0 + k * spread) for k in range(len(lights))]
        link.send_frame(frame_for(levels, lights, level, colours))
        if t - last_beat > 0.1:
            link.beat()
            last_beat = t
        time.sleep(0.02)
    print("  sent %d frames carrying %d outputs" % (link.sent_frames, link.sent_outputs))


def movement_watchdog(link, levels, lights):
    """Stop talking, and watch everything go safe on its own.

    The single most important behaviour on the board, and the only one that
    matters when this script is what crashes. Nothing is sent after the setup:
    that is the test.
    """
    print("watchdog, everything lit, then silence")
    for k, d in enumerate(lights):
        rgb = PALETTE[k % len(PALETTE)][1]
        link.cue(d["id"], {"r": rgb[0], "g": rgb[1], "b": rgb[2]}, hold_ms=60000)
    for d in levels:
        link.cue(d["id"], {"intensity": 0.8}, hold_ms=60000)
    hold(link, 1.0)
    print("  silence now; everything should go dark and still within %dms" % WATCHDOG_MS)
    time.sleep(WATCHDOG_MS * 4 / 1000.0)
    print("  anything still lit or still spinning is a watchdog not doing its job")


MOVEMENTS = {
    "rollcall": movement_rollcall,
    "names": movement_names,
    "channels": movement_channels,
    "contrast": movement_contrast,
    "swap": movement_swap,
    "bundle": movement_bundle,
    "watchdog": movement_watchdog,
}
ORDER = ["rollcall", "names", "channels", "contrast", "swap", "bundle", "watchdog"]


def run(host, secret, only=None):
    link = Link(host, secret)

    instruments = announced(link)
    if instruments is None:
        print("no answer to hello. Either the secret is wrong, or something")
        print("else is already talking to this board and has taken the replay")
        print("counter past where this client starts. Try poke.py hello first:")
        print("it says which of those it is.")
        link.close()
        return 1
    if not instruments:
        print("this board announces nothing attached, so there is nothing to")
        print("drive. Configure it from the studio's Boards page first.")
        link.close()
        return 1

    print("attached:")
    describe(instruments)
    levels, lights = sort_out(instruments)
    if not levels and not lights:
        print("nothing here takes one or three channels, so this script has")
        print("nothing to say about it.")
        link.close()
        return 1
    print()

    before = counters(host, secret)
    for name in ([only] if only else ORDER):
        MOVEMENTS[name](link, levels, lights)
        print()

    link.send({"t": "safe"})
    print("safe")

    report(link, before, counters(host, secret))
    link.close()
    return 0


def report(link, before, after):
    if not before or not after:
        print()
        print("no counters to compare, so this run is worth exactly what you")
        print("saw with your own eyes.")
        return
    got_cues = after["cues"] - before["cues"]
    # The board's counter increments once per output, not once per frame,
    # despite the page calling them frames: a bundle carrying three outputs
    # counts three. Compared against outputs here so the number means
    # something. Worth fixing on the board; it is a label rather than a fault.
    got_outputs = after["outputs"] - before["outputs"]
    refused = after["refused"] - before["refused"]

    print()
    print("the board's own count of this run:")
    print("  cues       sent %d, applied %d" % (link.sent_cues, got_cues))
    print("  outputs    sent %d in %d frames, applied %d" % (
        link.sent_outputs, link.sent_frames, got_outputs))
    print("  refused    %d datagrams" % refused)
    if got_cues < link.sent_cues:
        print("  a cue that was not applied is a cue for an instrument this")
        print("  board does not have, or one it refused. Check the ids above.")
    if link.sent_outputs and got_outputs < link.sent_outputs:
        lost = 100.0 * (link.sent_outputs - got_outputs) / link.sent_outputs
        print("  %.0f%% lost, which is wifi and is expected in small amounts." % lost)
        print("  Above about 5% something is wrong with the link rather than")
        print("  with the protocol.")
    if refused:
        print()
        print("  %d refusals, and this run is the only thing that should have" % refused)
        print("  been talking. The node keeps one replay counter for the whole")
        print("  board rather than one per sender, and every client seeds its")
        print("  counter from the clock, so a client that connects later starts")
        print("  hundreds of thousands of counts ahead and silences the earlier")
        print("  one for good. Opening the studio's Boards page during this run")
        print("  does it. So does a show: the conductor goes quiet and stays")
        print("  quiet, and nothing on either side says why.")
        print("  Cues before the other client spoke still land, and curve")
        print("  frames are unaffected because they carry no counter, which is")
        print("  what makes this so quiet a failure.")


if __name__ == "__main__":
    if len(sys.argv) < 3:
        raise SystemExit(__doc__)
    where, key = sys.argv[1], sys.argv[2]
    what = sys.argv[3] if len(sys.argv) > 3 else "all"

    if what == "hello":
        conn = Link(where, key)
        found = announced(conn)
        if found is None:
            raise SystemExit("no answer")
        describe(found)
        conn.close()
    elif what == "all":
        raise SystemExit(run(where, key))
    elif what in MOVEMENTS:
        raise SystemExit(run(where, key, only=what))
    else:
        raise SystemExit(__doc__)
