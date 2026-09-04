/* What can be attached to a pin, and what it can be told.
 *
 * Three types, which is what an ESP32 usefully drives: a dimmed output, an
 * addressable strip, and a switch. See ADR 0007 for why there is no build time
 * selector between them yet, and docs/cip.md for the configuration that says
 * which is on which pin.
 *
 * The types are what this build contains. The devices are what a configuration
 * says is plugged into it, and that includes the physical facts about the thing
 * on the end of the wire: how long it takes to do anything, how long it takes
 * to get there, and what it should be doing when nobody is talking to it. Those
 * used to be #defines, which is why the fan's declared 1.2 second dead time has
 * been a guess since the day it was written: measuring it meant editing this
 * file and reflashing, so nobody ever did.
 */

#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "driver/ledc.h"
#include "led_strip.h"

#define DEVICE_MAX      8
#define DEVICE_ID_MAX   32
#define DEVICE_KIND_MAX 16
#define DEVICE_CHANNELS 3

typedef enum {
    DEV_NONE = 0,
    DEV_PWM,
    DEV_WS28XX,
    DEV_RELAY,
} device_type_t;

typedef struct {
    char          id[DEVICE_ID_MAX];
    char          kind[DEVICE_KIND_MAX];
    device_type_t type;
    int           gpio;

    /* pwm */
    int freq_hz;
    /* ws28xx */
    int  pixels;
    char order[8];
    /* relay */
    bool active_high;

    /* What the thing on the end of the wire actually does. Declared to the
     * conductor, which fires every cue latency_ms early on the strength of it.
     * An instrument that lies here is the easiest way to make a rig feel wrong
     * in a way nobody can diagnose from the room. */
    float latency_ms;
    float ramp_up_ms;
    float ramp_down_ms;
    float safe;

    /* --- runtime, not configuration --- */

    /* channels is 1 for a dimmed output or a switch, 3 for a strip. */
    int   channels;
    float value[DEVICE_CHANNELS];
    /* hold_until_us is when a span this device was given must end, whether or
     * not a stop ever arrives. Per device: a four second fog burst ending must
     * not stop a fan in the middle of a scene. */
    int64_t hold_until_us;
    bool    is_safe;

    ledc_channel_t    ledc;
    led_strip_handle_t strip;
} device_t;

/** The name a configuration uses for a type, and that a node announces. */
const char *device_type_name(device_type_t t);

/**
 * One device, as the JSON a node announces for it.
 *
 * Everything a configuration can set is in here, which is the property that
 * matters: a field a board stores and does not announce reads back as empty,
 * and the next write clears it.
 */
struct cJSON *device_announcement(const device_t *d, int index);

/** Why a pin cannot be used, or NULL when it can. */
const char *device_pin_problem(int gpio);

/** The name of a type, or DEV_NONE for something this build does not have. */
device_type_t device_type_of(const char *name);

/**
 * Bring a device up on its pin.
 *
 * Leaves it at its safe value, because the window between a pin being
 * configured and being commanded is a window where a fogger could be on.
 */
bool device_start(device_t *d);

/** Release whatever the device holds, so a reconfiguration can reuse the pin. */
void device_stop(device_t *d);

/** Drive the device to the values it is currently holding. */
void device_apply(device_t *d);

/** Put one device at its safe value, now. */
void device_safe(device_t *d);
