/* What the board can say about itself.
 *
 * Exists so that the web page does not reach into the node's variables. The
 * devices live behind a mutex and are rewritten whenever a configuration
 * arrives, so anything reading them from another task has to be handed a copy
 * taken under the lock, not a pointer to the real thing.
 *
 * The secret is asked about here rather than handed out, for the same reason:
 * it stays inside componium_node.c, and everything else can only ask whether
 * something matches it.
 */

#ifndef COMPONIUM_STATUS_H
#define COMPONIUM_STATUS_H

#include <stdbool.h>
#include <stdint.h>

/** One device, copied out from under the lock. */
typedef struct {
    int   index;
    char  id[32];
    char  kind[24];
    const char *type;      /* "pwm", "ws28xx", "relay" */
    int   gpio;
    int   channels;
    float value[3];
    bool  is_safe;
    float latency_ms;
    int   hold_ms_left;    /* 0 when nothing is holding it */
} status_device_t;

/** Copy out what is attached. Returns how many were written. */
int node_status_devices(status_device_t *out, int max);

/** Counters, all since boot. */
void node_status_counters(uint32_t *cues, uint32_t *curves, uint32_t *refused,
                          int64_t *since_heartbeat_ms);

/** Least free stack either long lived task has had, in bytes. 0 if not running. */
void node_status_stacks(unsigned *serve, unsigned *watchdog);

/** Whether this build has a secret at all. */
bool node_secret_required(void);

/**
 * Whether a candidate is the secret.
 *
 * Compared in constant time, and the comparison stays in here so the secret
 * itself never leaves this translation unit.
 */
bool node_secret_matches(const char *candidate);

#endif
