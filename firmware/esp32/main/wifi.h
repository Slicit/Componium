/* Joining a network, and remembering how.
 *
 * Credentials live in NVS on the device and arrive over the USB cable from
 * whoever is holding the board. They are never in this repository, never in a
 * build, and never in a log line: a firmware image with somebody's Wi-Fi
 * password baked into it is an image that cannot be shared, and the whole
 * point of the web installer is that the image can be.
 */

#pragma once

#include <stdbool.h>
#include <stddef.h>
#include "esp_err.h"

/** Bring up the station and connect if credentials are already stored. */
esp_err_t wifi_start(void);

/** Whether we currently hold an address. */
bool wifi_connected(void);

/** Block until connected, or give up. True if we got there. */
bool wifi_await(uint32_t timeout_ms);

/**
 * Try these credentials, and store them only if they work.
 *
 * Storing first and testing later leaves a device that boots into a network
 * it cannot join and has forgotten the one it could, which on a board with no
 * screen and no buttons is indistinguishable from a brick.
 */
bool wifi_try(const char *ssid, const char *pass, uint32_t timeout_ms);

/** The dotted quad, or an empty string when there is no address. */
void wifi_address(char *out, size_t n);

/** Told about one network in range. */
typedef void (*wifi_seen)(const char *ssid, int rssi, bool secured, void *ctx);

/**
 * Look around, and report what is there. Blocks for a few seconds.
 *
 * Worth having so that a person can pick their network from a list rather than
 * type it from memory: a mistyped SSID and a network out of range are the same
 * silence from the board's side, and the board has no screen to tell them
 * apart with.
 */
bool wifi_scan(wifi_seen each, void *ctx);
