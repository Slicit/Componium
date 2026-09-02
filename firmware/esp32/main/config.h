/* What is attached to this board, and where it is remembered. */

#pragma once

#include <stdbool.h>
#include <stddef.h>

#include "devices.h"

/** How much configuration a board will hold. */
#define CONFIG_JSON_MAX 4096

/**
 * Read a configuration into devices.
 *
 * Returns how many, or -1 with a sentence in problem saying what was wrong.
 * Refused whole rather than in part: half a configuration is a board that looks
 * set up and is not.
 */
int config_parse(const char *json, device_t *out, char *problem, size_t problem_len);

/** Remember a configuration across reboots. */
bool config_save(const char *json);

/** Read back what was remembered. Returns its length, or 0 for none. */
int config_load(char *json, size_t len);

/** Forget it, so the board comes back up announcing nothing. */
void config_forget(void);

/** Start counting finite peripherals again, before applying a configuration. */
void device_reset_budget(void);
