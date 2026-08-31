#!/usr/bin/env python3
"""Label one film frame with a vision model, for --vlm-command.

    compose.py film.mkv --vlm-command "hack/vlm-label.py"

The composer hands this an image path and reads labels from stdout, one per
line. That contract is the whole interface; this script is one implementation
of it and nothing in the composer knows it exists.

Configured entirely by environment, because --vlm-command is a single string
and threading options through it would mean inventing a second command line:

    COMPONIUM_VLM_HOST   default http://gaming.home:14242
    COMPONIUM_VLM_MODEL  default qwen3-vl:8b
    COMPONIUM_VLM_DEBUG  set to print the raw reply on stderr

The vocabulary below is not a description of the world. It is exactly the set
of words the composer can already act on, plus the ones this feature adds, and
nothing else: a model that answers "pyroclastic flow" is producing a word that
maps to no instrument, which is the same as saying nothing while costing five
seconds. So the prompt names the list and the parser rejects everything off it.
"""

from __future__ import annotations

import base64
import json
import os
import re
import socket
import sys
import urllib.error
import urllib.request

HOST = os.environ.get("COMPONIUM_VLM_HOST", "http://gaming.home:14242").rstrip("/")
# Which server is on the other end. Ollama and an OpenAI compatible server
# differ in the shape of the request and in nothing that matters here, so the
# prompt, the vocabulary and the parser are shared and only the envelope
# changes. vLLM speaks the OpenAI shape.
API = os.environ.get("COMPONIUM_VLM_API", "ollama").strip().lower()
MODEL = os.environ.get("COMPONIUM_VLM_MODEL", "qwen3-vl:8b")
DEBUG = bool(os.environ.get("COMPONIUM_VLM_DEBUG"))

# What this film is, in the operator's own words.
#
# Free text: a genre, a line of synopsis, the names of two characters. It is
# handed to the model as background for the sentence it writes and for nothing
# else. Empty is the normal case and changes nothing.
CONTEXT = os.environ.get("COMPONIUM_VLM_CONTEXT", "").strip()
TIMEOUT = float(os.environ.get("COMPONIUM_VLM_TIMEOUT", "120"))

# What the composer can do something with. Anything else is noise.
EFFECTS = (
    "explosion", "lightning", "fire", "smoke", "dust", "splash", "rain",
)
SCENES = ("calm", "active")

# The prompt is the feature.
#
# Three things it has to fight, all learned by running it:
#
# A list of things to look for is read as an invitation to find them. Naming
# what each word does NOT cover is worth more than defining what it does.
#
# The difference between a setting and an event. A seascape contains water in
# every frame and warrants no effect; a wave breaking over the camera does.
# "water" is a confirmation that a blue scene really is water, and "splash" is
# the event — two different jobs that a model will happily conflate.
#
# Calm is the answer for most of a film, and a model asked to judge tends
# toward the more interesting option. So calm is defined as the default and
# active has to be earned.
PROMPT = """You are labelling one frame from a film so a machine can drive
physical effects — fans, lights, water, smoke — for the audience watching it.

Two of the lines below are evidence and two are judgement. They are answered
differently and the difference matters.

Reply with exactly five lines and nothing else. No explanation.

EFFECTS: <comma-separated words from the list below, or the word none>
WATER: <yes if any sea, lake, river or large stretch of water is visible
        anywhere in the frame, however calm, otherwise no>
SCENE: <calm or active>
SEEN: <one plain sentence describing what is in the frame and what is
       happening, as you would tell someone who cannot see it>
LIKELY: <what this frame appears to be part of, in a few words, or the word
         none if it suggests nothing beyond itself>

EFFECTS and WATER are evidence. Report only what is plainly visible IN THIS
FRAME. Do not infer them from the story, from what a film of this kind usually
contains, or from your own answers on the lines below. A war film is not a
reason to report an explosion, and a frame with no fireball in it has none.

The only permitted effect words:

  explosion  a fireball or blast going off. Not a fire that is merely burning.
  lightning  a lightning bolt, or a flash so bright it lights the whole scene.
  fire       visible flames. Not smoke alone, not glowing embers.
  smoke      a plume or cloud of smoke. Not fog, mist, haze or low cloud.
  dust       a burst of dust or debris thrown into the air by an impact.
  splash     water thrown into the air: spray, a breaking wave, something
             hitting water. The water must be moving.
  rain       rain visibly falling.

If none of those are plainly visible, answer exactly: EFFECTS: none

SCENE is a judgement rather than evidence: how much is happening to the
audience, not how pretty the frame is.

  calm    conversation, stillness, scenery, walking, slow camera movement.
  active  impact, fast motion, combat, chaos, a vehicle at speed, destruction.

One frame is a poor sample of movement, so judge the moment the frame comes
from rather than the pixels alone. Two figures frozen mid-stride with weapons
in a ruined street is active, though nothing in the still is blurred; the same
two seated at a table is calm.

Most frames of most films are calm. Answer active only when something is
actually happening.

LIKELY is where anything you have inferred belongs — a battle, a chase, a
funeral, a briefing, a landing. It drives nothing at all. It is there so that
a person reading several thousand of these can tell one scene from another
without watching the film."""


# What the film is, added to the prompt when somebody has said.
#
# Only the sentence may use it, and the prompt says so twice: once as a rule and
# once as the reason. A model told it is watching a war film will find
# explosions in a frame that has none if it is allowed to reason that way, which
# is the same fault as emphasising a label and getting more of that label.
#
# What it is for is the other half: "a man in a military uniform in a dimly lit
# room" is what a model says when it does not know it is looking at a hangar
# deck, and a person reading three thousand of those cannot tell the film apart
# from any other film.
CONTEXT_PROMPT = """

About this film, from the person who set it up:

  %s

Use it for SEEN, SCENE and LIKELY: to name what you are looking at, and to
judge whether the moment is calm or active.

It is not evidence for EFFECTS or WATER. Those are about this frame and
nothing else. A film being a war film is not a reason to report an explosion,
and a film set at sea is not a reason to report water. If the frame does not
show it, it is not there."""


def prompt() -> str:
    """The prompt, with the film's own context when there is any."""
    if not CONTEXT:
        return PROMPT
    return PROMPT + CONTEXT_PROMPT % CONTEXT

# The sentence comes last, after the labels are settled.
#
# Asked first it changed the answers, and for the worse: the model conditioned
# its labels on its own narration, wrote "a sandy beach" and then reported
# water on a desert frame, and turned a crab kicking up sand back into a
# splash. Asked last, every label matches what the model gives with no sentence
# at all — and the sentence itself is better, because it is describing what was
# decided rather than deciding it. It says the crabs are kicking up sand.

# Water is asked separately because it is not an effect and the prompt spends
# its first paragraphs insisting on effects.
#
# Listed among them it was simply never reported: a still sea on the horizon is
# the least event-like thing in a frame, and two rewordings inside the list
# changed nothing at all — the model would not put a setting in a line called
# EFFECTS. Asked as its own question it answers correctly on every frame tried,
# water and dry alike. The distinction is real, so the prompt now has it too.


# Prefer IPv4, because a name on a home LAN is often not one address.
#
# gaming.home resolves to four IPv6 addresses and two IPv4 ones here, and all
# four of the IPv6 addresses are stale: nothing answers on them. Python tries
# what getaddrinfo returns, in the order it returns them, with a full TCP
# timeout each — so a call Ollama serviced in 1.4 seconds took 146, being four
# dead addresses at roughly 36 seconds apiece before falling through to the one
# that works. curl hides this with Happy Eyeballs; urllib does not.
#
# Sorting IPv4 first is the smallest fix that is still correct: IPv6 is not
# removed, only tried second, so this still works on a network where IPv6 is
# the only route. Set COMPONIUM_VLM_IPV6_FIRST to leave the order alone.
_getaddrinfo = socket.getaddrinfo


def _ipv4_first(*args, **kwargs):
    return sorted(_getaddrinfo(*args, **kwargs),
                  key=lambda r: 0 if r[0] == socket.AF_INET else 1)


if os.environ.get("COMPONIUM_VLM_IPV6_FIRST") is None:
    socket.getaddrinfo = _ipv4_first


def encode(path: str) -> str:
    with open(path, "rb") as f:
        return base64.b64encode(f.read()).decode("ascii")


def ask(image_path: str) -> str:
    """Ask the model about one frame, with the answer already begun.

    The reply is prefilled with "EFFECTS:" as an assistant turn the model
    continues. That is not a formatting nicety, it is what makes this usable:

    qwen3-vl reasons before answering, and its reasoning goes to a separate
    "thinking" field rather than to the response. It ignores think: false, and
    it only reasons on frames it finds ambiguous — so the failure is silent and
    intermittent, a frame coming back with no labels at all because the model
    thought carefully about it and then ran out of budget before saying
    anything. Prefilling leaves it nowhere to put a thought: the turn has
    already started, and it can only continue.

    Measured over the same frames: 2.5 to 4.3 seconds each and one silent
    failure in five, against 0.5 to 1.4 seconds and none.
    """
    if API in ("openai", "vllm"):
        return ask_openai(image_path)

    body = json.dumps({
        "model": MODEL,
        "stream": False,
        "think": False,
        "messages": [
            {"role": "user", "content": prompt(), "images": [encode(image_path)]},
            {"role": "assistant", "content": "EFFECTS:"},
        ],
        # Zero temperature because this is a classification, and a label that
        # changes between runs makes a score that cannot be reproduced.
        "options": {"temperature": 0, "num_predict": 220},
    }).encode()
    req = urllib.request.Request(
        HOST + "/api/chat", data=body,
        headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        reply = json.loads(r.read()).get("message", {}).get("content", "")
    # The prefill is not echoed back, so the first line arrives without the
    # "EFFECTS:" the parser looks for. Put it back rather than teaching the
    # parser about a special first line.
    return "EFFECTS:" + reply


def ask_openai(image_path: str) -> str:
    """The same question, in the shape vLLM and friends expect.

    Two differences worth naming. The image travels as a data URI inside the
    message rather than as a separate array, and there is no prefill: the
    OpenAI chat shape has no way to begin the assistant's turn, so the answer
    is not started for it.

    That matters because prefilling is what stopped qwen3-vl reasoning itself
    out of an answer. A model that does not reason before replying does not
    need it — but if one does, this is where it will show, as an empty reply
    with a stop reason of length.
    """
    body = json.dumps({
        "model": MODEL,
        "temperature": 0,
        "max_tokens": 220,
        "messages": [{
            "role": "user",
            "content": [
                {"type": "text", "text": prompt()},
                {"type": "image_url",
                 "image_url": {"url": "data:image/jpeg;base64," + encode(image_path)}},
            ],
        }],
    }).encode()
    req = urllib.request.Request(
        HOST + "/v1/chat/completions", data=body,
        headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        payload = json.loads(r.read())
    choices = payload.get("choices") or []
    if not choices:
        return ""
    return choices[0].get("message", {}).get("content", "") or ""


def parse(reply: str) -> list[str]:
    """Pull the permitted words out of a reply, and nothing else.

    Deliberately forgiving about shape and merciless about vocabulary. Models
    wander in formatting — a bullet, a stray "the", markdown bold — and none of
    that changes the answer. Inventing a word does.
    """
    out: list[str] = []
    for line in reply.splitlines():
        line = line.strip().lower().lstrip("-*# ").strip()
        head, _, rest = line.partition(":")
        head = head.strip().strip("*")
        if head == "effects":
            for word in re.split(r"[^a-z]+", rest):
                if word in EFFECTS and word not in out:
                    out.append(word)
        elif head == "water":
            # Emitted as a plain "water" label, because that is what the rest
            # of the composer already reads: it confirms a blue scene really
            # is water, and it is what the splash gate corroborates against.
            # The question changed shape, not the answer.
            if "yes" in re.split(r"[^a-z]+", rest) and "water" not in out:
                out.append("water")
        elif head == "scene":
            for word in re.split(r"[^a-z]+", rest):
                if word in SCENES:
                    # Prefixed so it cannot be mistaken for an effect by
                    # anything downstream that reads these as a flat list.
                    tag = "scene-" + word
                    if tag not in out:
                        out.append(tag)
    return out


def described(reply: str) -> str:
    """The model's own sentence about the frame, or nothing.

    Kept because it is the part that cannot be reconstructed. The labels are a
    conclusion and can be drawn again from a rerun; the sentence is what was
    there, and once the film has been decoded and thrown away it is the only
    record of it. It costs about eighty bytes.
    """
    for line in reply.splitlines():
        head, _, rest = line.strip().lstrip("-*# ").partition(":")
        if head.strip().strip("*").lower() == "seen":
            return " ".join(rest.split())
    return ""


def inferred(reply: str) -> str:
    """What the model thought the frame was part of, or nothing.

    Kept apart from the sentence because they are different claims. SEEN is
    what is there; this is what it looks like it belongs to, and a reader has
    to be able to tell which is which — otherwise the guess reads as the
    observation and the description becomes less trustworthy rather than more.
    """
    for line in reply.splitlines():
        head, _, rest = line.strip().lstrip("-*# ").partition(":")
        if head.strip().strip("*").lower() == "likely":
            said = " ".join(rest.split())
            return "" if said.lower() in ("", "none", "nothing") else said
    return ""


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        sys.stderr.write("usage: vlm-label.py IMAGE\n")
        return 2
    try:
        reply = ask(argv[1])
    except (urllib.error.URLError, OSError, TimeoutError) as e:
        # Silence on stdout, complaint on stderr. The composer treats a
        # failed frame as an unlabelled one and carries on, which is what it
        # should do: a model being down is not a reason to lose the analysis.
        sys.stderr.write("vlm: %s\n" % e)
        return 1
    if DEBUG:
        sys.stderr.write("vlm raw: %r\n" % reply)
    for label in parse(reply):
        print(label)
    seen = described(reply)
    if seen:
        # A comment line, because the seam has always ignored those — so a
        # description costs nothing to anything that was reading labels before
        # this existed, and the composer picks it up only because it now looks.
        print("# " + seen)
    guess = inferred(reply)
    if guess:
        # Prefixed, where the sentence is bare. The seam has carried one
        # unmarked comment since descriptions existed, so leaving that alone
        # keeps every older reader working and gives the new line somewhere
        # unambiguous to go.
        print("# likely: " + guess)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
