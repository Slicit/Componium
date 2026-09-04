/* The edge of the board, where somebody else's numbers become ours.
 *
 * Everything here exists because this device has no memory protection worth the
 * name, no screen, and no way back in except a USB cable. A crash is not an
 * exception that gets logged and retried: it is a reboot, in a room, in the
 * dark, halfway through a film, with a fan that stops and a fogger whose state
 * nobody can see.
 *
 * So the rule at this boundary is that no value from outside is trusted to be
 * in range, in shape, or even to be a number at all. Not because an attacker is
 * assumed, though one is cheap to assume on a LAN, but because a conductor with
 * a bug sends exactly the same bytes as an attacker does.
 */

#ifndef COMPONIUM_GUARD_H
#define COMPONIUM_GUARD_H

#include <stdbool.h>
#include <stddef.h>

/* How deep a JSON document from the network may nest.
 *
 * cJSON's parser is recursive and its own limit is 1000, which is a stack
 * overflow on any task this board has: a thousand frames of parser state is
 * tens of kilobytes and the largest stack here is eight. Nothing this protocol
 * describes is deeper than about five, so sixteen is generous and still refuses
 * the document whose only purpose is to be deep.
 */
#define JSON_MAX_DEPTH 16

/* Whether a document is shallow enough to hand to the parser.
 *
 * Checked before parsing rather than during, because the crash happens inside
 * the parser and there is no reporting a stack overflow after the fact. A
 * linear scan over bytes cannot itself recurse, which is the whole point.
 *
 * Understands strings and their escapes, so a brace inside "{{{{" is text.
 */
bool json_shallow_enough(const char *text, int len, int max_depth);

/* A number from outside, made safe to drive an output with.
 *
 * NaN becomes zero. It is not a value, it compares false against every bound,
 * so it walks through `if (v < 0)` and `if (v > 1)` untouched and arrives at
 * the duty register as whatever the cast happens to produce. Infinity and
 * everything above one become one; everything below zero becomes zero.
 *
 * Zero for NaN rather than refusing the cue, because a cue that is refused
 * leaves the output where it was, and where it was is a fan already running.
 */
float unit_value(double v);

/* An integer from outside, held between two bounds it must not leave. */
int bounded_int(double v, int lo, int hi, int fallback);

#endif
