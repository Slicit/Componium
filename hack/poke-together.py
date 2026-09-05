"""Drive every output on one board at once, over authenticated CIP 0.3.

The companion to poke.py, which speaks 0.2 unauthenticated and drives one
output at a time. That was the right tool when a node was an instrument. A node
now carries several, so the interesting question moved: not "does the fan
spin", which poke.py already answers, but "do the fan and the light move
together", which nothing answered.

    python3 poke-together.py 192.168.1.75 <secret>          # the whole thing
    python3 poke-together.py 192.168.1.75 <secret> hello    # just look

Three movements, and each one fails differently on purpose:

  cues       Both outputs told separately, one after the other. This is the
             control: if this does not work, nothing about simultaneity is
             worth measuring yet.
  bundle     Both outputs in one datagram, fifty times a second. The node
             applies every output in a frame before it returns, so this is
             the only way simultaneous is actually simultaneous over a
             transport that drops and reorders.
  watchdog   The heartbeats stop. Both outputs should go safe together and
             within 300ms, without being told to.

The counters come off the board's own status page afterwards, so the report is
what the board did rather than what this sent. A curve frame is disposable by
design and some will be lost; the number says how many.

One warning worth reading. The node keeps a single replay counter for the whole
board rather than one per sender, so two clients cannot talk to it at once: the
one that connected earlier has every datagram refused from the moment the later
one speaks. Running this against a board that a conductor is currently playing
will silence that conductor until it reconnects. That is a property of the
node, not of this script, and this script is a convenient way to watch it
happen.
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
                body += struct.pack(">f", v)
        self.sock.sendto(self.wrap(bytes(body)), (self.host, CIP_PORT))
        self.sent_frames += 1
        self.sent_outputs += len(outputs)

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
                continue        # somebody else's datagram, or a curve frame
        return None

    def beat(self):
        self.send({"t": "heartbeat"})

    def close(self):
        self.sock.close()


def hold(link, seconds, then=None):
    """Keep the outputs alive for a while, beating as the conductor would.

    The heartbeats are the point of this helper. Without them the node drops
    everything to safe after 300ms, which looks exactly like a cue that never
    landed.
    """
    until = time.time() + seconds
    while time.time() < until:
        link.beat()
        if then:
            then(1.0 - (until - time.time()) / seconds)
        time.sleep(0.02)


# --- what is attached ------------------------------------------------------

def announced(link):
    reply = link.ask({"t": "hello"})
    if reply is None:
        return None
    return reply.get("instruments") or []


def channels_of(inst):
    return len(inst.get("channels") or [])


def pick(instruments):
    """Choose one single valued output and one colour output.

    By what the board announced rather than by name. A board is configured by
    whoever set it up and the ids are theirs, so matching on "wind.main" would
    work on this bench and nowhere else. Kind first, because that is the field
    that means something; channel count as the fallback, because three channels
    is a colour whatever it is called.
    """
    lights = [i for i in instruments if i.get("kind") == "light"]
    others = [i for i in instruments if i.get("kind") != "light"]
    light = next((i for i in lights if channels_of(i) == 3), None)
    if light is None:
        light = next((i for i in instruments if channels_of(i) == 3), None)
    wind = next((i for i in others if channels_of(i) == 1), None)
    if wind is None:
        wind = next((i for i in instruments
                     if channels_of(i) == 1 and i is not light), None)
    return wind, light


def describe(instruments):
    for i in instruments:
        print("  %d  %-16s %-7s %-7s gpio %-3s %d channel(s)" % (
            i.get("index", -1), i.get("id", "?"), i.get("kind", "?"),
            i.get("type", "?"), i.get("gpio", "?"), channels_of(i)))


# --- the board's own account of it -----------------------------------------

def counters(host, secret):
    """Cues applied, curve frames received and datagrams refused, per the board.

    Read off the status page on port 80, which is the only place the node
    reports them. Scraped, because the page is for a person and this is the
    one machine reading it; if that ever matters, hello should carry them.
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
    return {"cues": int(found.group(1)), "frames": int(found.group(2)),
            "refused": int(found.group(3))}


# --- the three movements ---------------------------------------------------

def movement_cues(link, wind, light):
    """Both outputs told separately, then held together.

    Two cues rather than one, because a cue names one instrument. Each carries
    a hold long enough to outlive the movement, so that what ends this is the
    next movement rather than the node's own expiry.
    """
    print("cues, one instrument each")
    if wind:
        print("  %s to 0.6" % wind["id"])
        link.send({"t": "cue", "seq": 1, "instrument": wind["id"],
                   "params": {"intensity": 0.6}, "hold_ms": 4000})
        link.sent_cues += 1
    if light:
        print("  %s to amber" % light["id"])
        link.send({"t": "cue", "seq": 2, "instrument": light["id"],
                   "params": {"r": 1.0, "g": 0.45, "b": 0.0}, "hold_ms": 4000})
        link.sent_cues += 1
    hold(link, 2.5)


def movement_bundle(link, wind, light, seconds=6.0):
    """Both outputs in one datagram, fifty times a second.

    The fan ramps up and back down while the light walks around the hue circle.
    Two motions that are easy to tell apart by eye, so that one output lagging
    the other is visible rather than merely measurable.
    """
    print("one bundle, both outputs, 50Hz for %.0fs" % seconds)
    start = time.time()
    last_beat = 0.0
    while True:
        t = time.time() - start
        if t >= seconds:
            break

        outputs = []
        if wind:
            # Up over the first half and back down over the second, so the
            # ramp is unmistakably a ramp and ends where it started.
            phase = t / seconds
            level = 2 * phase if phase < 0.5 else 2 * (1 - phase)
            outputs.append((wind["index"], [max(0.0, min(1.0, level))]))
        if light:
            outputs.append((light["index"], hue(t / 2.0)))
        if outputs:
            link.send_frame(outputs)

        # Heartbeats are separate from frames on purpose: a board driven only
        # by curve frames still has to be told the conductor is alive, and a
        # rig that conflated the two would keep running on a stream that was
        # nothing but stale repeats.
        if t - last_beat > 0.1:
            link.beat()
            last_beat = t
        time.sleep(0.02)
    print("  sent %d frames" % link.sent_frames)


def movement_watchdog(link, wind, light):
    """Stop talking, and watch both outputs go safe on their own.

    The single most important behaviour on the board, and the only one that
    matters when this script is what crashes. Nothing is sent here: that is the
    test.
    """
    print("watchdog, no heartbeats for %dms" % (WATCHDOG_MS * 4))
    if wind:
        link.send({"t": "cue", "seq": 3, "instrument": wind["id"],
                   "params": {"intensity": 0.8}, "hold_ms": 60000})
        link.sent_cues += 1
    if light:
        link.send({"t": "cue", "seq": 4, "instrument": light["id"],
                   "params": {"r": 0.0, "g": 0.0, "b": 1.0}, "hold_ms": 60000})
        link.sent_cues += 1
    hold(link, 1.0)
    print("  silence now; both should fall to safe within %dms" % WATCHDOG_MS)
    time.sleep(WATCHDOG_MS * 4 / 1000.0)
    print("  if either is still running, the watchdog is not doing its job")


def hue(turns):
    """A point on the hue circle as rgb in 0..1, at full saturation."""
    h = (turns % 1.0) * 6.0
    c = 1.0
    x = c * (1 - abs(h % 2 - 1))
    table = [(c, x, 0), (x, c, 0), (0, c, x), (0, x, c), (x, 0, c), (c, 0, x)]
    return list(table[int(h) % 6])


def run(host, secret):
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
    wind, light = pick(instruments)
    if not wind and not light:
        print("nothing here takes one or three channels, so this script has")
        print("nothing to say about it.")
        link.close()
        return 1
    if not wind or not light:
        print("only one of the two kinds is attached, so this runs but proves")
        print("nothing about simultaneity.")
    print()

    before = counters(host, secret)

    movement_cues(link, wind, light)
    movement_bundle(link, wind, light)
    movement_watchdog(link, wind, light)

    link.send({"t": "safe"})
    print("safe")

    after = counters(host, secret)
    report(link, before, after)
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
    # despite the page calling them frames: a bundle carrying two outputs
    # counts two. Compared against outputs here so the number means something.
    # Worth fixing on the board, and it is a label rather than a fault.
    got_outputs = after["frames"] - before["frames"]
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
    else:
        raise SystemExit(__doc__)
