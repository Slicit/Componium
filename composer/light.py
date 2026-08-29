"""Ambient light in two layers.

A single light track cannot do both jobs. If it follows the picture closely
enough to feel like ambient light, it has no headroom left for a lightning
strike; if it is scaled for lightning, the ambient half sits at a tenth of
brightness and looks broken.

So there are two:

  **Soft.** A slow, desaturated, deliberately *ceilinged* wash that follows the
  average colour of the picture. It never goes above SOFT_CEILING, which is
  what leaves room above it.

  **Event.** Short, bright, saturated spikes on lightning, explosions and cuts
  to a much brighter shot. These are allowed the full range, and they read as
  spikes precisely because the soft layer never does.

They are separate tracks addressed to separate instruments, so a rig can put
them on different fixtures: a wash behind the screen and something punchier
overhead, which is how a real installation would want it anyway.
"""

from __future__ import annotations

# The soft layer's ceiling. Two thirds leaves half a stop of headroom, which is
# enough for an event to be unmistakably brighter without the wash looking dim.
SOFT_CEILING = 0.65

# How far toward grey the soft layer is pulled. An average frame colour is
# already muddy, and driving a fixture at full saturation from it produces a
# colour nobody chose.
SOFT_DESATURATION = 0.35


def desaturate(rgb, amount: float):
    r, g, b = rgb
    grey = (r + g + b) / 3.0
    return (
        r + (grey - r) * amount,
        g + (grey - g) * amount,
        b + (grey - b) * amount,
    )


def soft_curve(colours, gain: float = 1.0, ceiling: float = SOFT_CEILING,
               desaturation: float = SOFT_DESATURATION, window: int = 5):
    """Turn average frame colours into the gentle layer.

    Smoothed over a window, because ambient light that follows every cut is a
    strobe. Half a second is about the point where it stops reading as a
    response to the picture and starts reading as flicker.
    """
    if not colours:
        return []

    smoothed = []
    half = max(0, window // 2)
    for i in range(len(colours)):
        lo = max(0, i - half)
        hi = min(len(colours), i + half + 1)
        n = float(hi - lo)
        smoothed.append((
            sum(c[0] for c in colours[lo:hi]) / n,
            sum(c[1] for c in colours[lo:hi]) / n,
            sum(c[2] for c in colours[lo:hi]) / n,
        ))

    out = []
    for rgb in smoothed:
        r, g, b = desaturate(rgb, desaturation)
        out.append((
            min(ceiling, r * gain),
            min(ceiling, g * gain),
            min(ceiling, b * gain),
        ))
    return out


def flashes(frames, colours, fps: float, rise: float = 0.22,
            floor: float = 0.35, min_gap: float = 0.6, hold: float = 0.25):
    """Find sudden brightenings worth a spike.

    A flash is a large, fast rise in mean luminance that also ends up bright.
    Requiring both matters: fading up from black to a dim shot is a large rise
    and not a flash, and a bright shot that was already bright is not one
    either.

    min_gap stops a flickering sequence, which lightning frequently is, from
    producing forty cues where one is wanted.
    """
    if len(frames) < 2:
        return []

    out = []
    last_at = -1e9
    for i in range(1, len(frames)):
        prev = frames[i - 1].mean / 255.0
        cur = frames[i].mean / 255.0
        if cur - prev < rise or cur < floor:
            continue
        at = i / fps
        if at - last_at < min_gap:
            continue
        last_at = at

        # Colour the spike from the frame itself, so an explosion reads warm
        # and lightning reads cold, without anyone having to classify which.
        if i < len(colours):
            r, g, b = colours[i]
            peak = max(r, g, b, 0.001)
            # Pushed to full brightness, keeping the hue. A flash that is not
            # bright is not a flash.
            r, g, b = r / peak, g / peak, b / peak
        else:
            r = g = b = 1.0

        out.append({
            "t": at,
            "action": "flash",
            "params": {"r": round(r, 4), "g": round(g, 4), "b": round(b, 4)},
            "duration": hold,
            "source": f"luminance rose {cur - prev:.2f} to {cur:.2f}",
        })
    return out
