"""Talk to a node directly, without a score, a conductor or a studio.

For the ten minutes after a board first joins a network, when the question is
not "does the show work" but "is anything on the end of this wire at all".

    python3 poke.py 192.168.1.145 hello     # who are you
    python3 poke.py 192.168.1.145 light     # a colour sequence, over sACN
    python3 poke.py 192.168.1.145 fan       # ramp the PWM output, over CIP

Both protocols are spoken here exactly as the conductor speaks them, so an
effect that works from this and not from a show is a scoring problem, and an
effect that works from neither is a wiring problem. That is the whole point of
having it.
"""

import json
import socket
import struct
import subprocess
import sys
import time

CIP_PORT = 5570
SACN_PORT = 5568
CIP_VERSION = "0.2"


# --- sACN ------------------------------------------------------------------

def e131(universe, slots, sequence, source="componium poke"):
    """One E1.31 packet carrying a full universe of 512 slots."""
    p = bytearray(638)
    struct.pack_into(">HH", p, 0, 0x0010, 0x0000)          # preamble, postamble
    p[4:16] = b"ASC-E1.17\x00\x00\x00"                      # ACN identifier
    struct.pack_into(">H", p, 16, 0x7000 | (638 - 16))      # root flags/length
    struct.pack_into(">I", p, 18, 0x00000004)               # root vector
    p[22:38] = bytes(range(16))                             # CID, any 16 bytes
    struct.pack_into(">H", p, 38, 0x7000 | (638 - 38))      # framing flags
    struct.pack_into(">I", p, 40, 0x00000002)               # framing vector
    name = source.encode()[:63]
    p[44:44 + len(name)] = name
    p[108] = 100                                            # priority
    p[111] = sequence & 0xFF
    struct.pack_into(">H", p, 113, universe)
    struct.pack_into(">H", p, 115, 0x7000 | (638 - 115))    # DMP flags
    p[117] = 0x02                                           # DMP vector
    p[118] = 0xA1                                           # address type
    struct.pack_into(">HHH", p, 119, 0x0000, 0x0001, 513)   # first, increment, count
    p[125] = 0x00                                           # DMX start code
    p[126:126 + len(slots)] = bytes(slots)
    return bytes(p)


def light(host, universe=1, start=1, seconds=1.0):
    """Walk a strip through colours it is impossible to mistake."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    steps = [
        ("red", (255, 0, 0)), ("green", (0, 255, 0)), ("blue", (0, 0, 255)),
        ("white", (255, 255, 255)), ("half white", (128, 128, 128)),
        ("off", (0, 0, 0)),
    ]
    seq = 0
    for name, rgb in steps:
        slots = [0] * 512
        # Start addresses are 1 based, as every lighting desk numbers them.
        slots[start - 1:start - 1 + 3] = list(rgb)
        print("  %-11s rgb%s" % (name, rgb), flush=True)
        # Sent repeatedly, because a receiver that misses one datagram should
        # not sit on the previous colour for the whole step.
        until = time.time() + seconds
        while time.time() < until:
            seq = (seq + 1) & 0xFF
            sock.sendto(e131(universe, slots, seq), (host, SACN_PORT))
            time.sleep(0.04)
    sock.close()


# --- CIP -------------------------------------------------------------------

def ask(sock, host, message, wait=2.0):
    sock.sendto(json.dumps(message).encode(), (host, CIP_PORT))
    sock.settimeout(wait)
    try:
        data, _ = sock.recvfrom(2048)
        return json.loads(data.decode())
    except socket.timeout:
        return None


def reachable(host, timeout=2):
    """Whether the host answers at all, as distinct from answering CIP.

    The distinction this whole file exists to draw. A board that is off, asleep,
    or on the wrong network looks exactly like a board with broken firmware if
    nobody asks the cheaper question first.
    """
    try:
        socket.gethostbyname(host)
    except socket.gaierror:
        return None          # not even a name we can resolve
    try:
        done = subprocess.run(
            ["ping", "-c", "1", "-W", str(timeout), host],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=timeout + 2)
        return done.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return None          # no ping to run, so we genuinely do not know


def hello(host):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    reply = ask(sock, host, {"v": CIP_VERSION, "t": "hello"})
    sock.close()
    if reply is not None:
        print(json.dumps(reply, indent=2))
        return True

    print("  no answer on udp/%d" % CIP_PORT)
    # Asked now, not assumed. Silence on a port and silence from a host are
    # different faults with different fixes, and guessing between them costs
    # somebody an hour with a serial monitor.
    up = reachable(host)
    if up is None:
        print("  and no way to tell whether %s is up from here" % host)
    elif up:
        print("  but %s answers ping, so the board is on the network and" % host)
        print("  nothing is listening for CIP: check it is running this")
        print("  firmware and got past its wifi")
    else:
        print("  and %s does not answer ping either, so the board is off," % host)
        print("  asleep, or on a different network. Nothing to do with CIP")
    return False


def fan(host, seconds=2.0):
    """Ramp the PWM output, holding it with heartbeats the whole way.

    The heartbeats are not optional and not a formality: the node drops its
    output to safe after 300ms without one. A ramp that stops mid way is the
    watchdog working, which is the single most important thing on the board.
    """
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    if not ask(sock, host, {"v": CIP_VERSION, "t": "hello"}):
        print("  no answer to hello; not driving anything")
        sock.close()
        return
    seq = 0
    for level in (0.3, 0.5, 0.7, 1.0, 0.5, 0.0):
        print("  intensity %.1f" % level, flush=True)
        seq += 1
        sock.sendto(json.dumps({
            "v": CIP_VERSION, "t": "cue", "seq": seq,
            "params": {"intensity": level},
            "hold_ms": int(seconds * 1000) + 500,
        }).encode(), (host, CIP_PORT))
        until = time.time() + seconds
        while time.time() < until:
            sock.sendto(json.dumps({"v": CIP_VERSION, "t": "heartbeat"}).encode(),
                        (host, CIP_PORT))
            time.sleep(0.1)
    sock.sendto(json.dumps({"v": CIP_VERSION, "t": "safe"}).encode(), (host, CIP_PORT))
    print("  safe")
    sock.close()


if __name__ == "__main__":
    if len(sys.argv) < 3:
        raise SystemExit(__doc__)
    where, what = sys.argv[1], sys.argv[2]
    if what == "hello":
        hello(where)
    elif what == "light":
        light(where, universe=int(sys.argv[3]) if len(sys.argv) > 3 else 1)
    elif what == "fan":
        fan(where)
    else:
        raise SystemExit(__doc__)
