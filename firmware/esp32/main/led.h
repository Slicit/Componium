/* An addressable strip on the same board as the fan.
 *
 * One ESP32, two instruments, two protocols, each the right one for its kind:
 * the fan takes CIP because a fan is a Componium instrument and nothing else
 * speaks to it, and the strip takes sACN because lighting already has a
 * protocol and LOGBOOK.md is explicit that we speak it rather than compete
 * with it. A rig entry for this strip is the same entry as for a WLED
 * controller with a different address in it, which is the point: the board is
 * a drop in substitute for one.
 */

#pragma once

#include <stdbool.h>
#include <stdint.h>

/* Where the data line is. Not 18, which is the fan. */
#ifndef LED_GPIO
#define LED_GPIO 5
#endif

/* How many pixels. Wrong here is not dangerous: too few leaves the tail dark,
 * too many wastes a little time per frame. */
#ifndef LED_COUNT
#define LED_COUNT 30
#endif

/** Bring up the strip, dark. False when there is no strip to bring up. */
bool led_start(void);

/** Paint the whole strip one colour and show it. */
void led_paint(uint8_t r, uint8_t g, uint8_t b);

/** Dark, now. The safe value for a light, and what silence resolves to. */
void led_off(void);
