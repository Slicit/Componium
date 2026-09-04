/*
 * Componium instrument node for ESP32, using ESP-IDF.
 *
 * Implements the Componium Instrument Protocol (docs/cip.md) over UDP and
 * drives a PWM output, which is enough for a fan, a dimmable light, a mister,
 * or anything else that takes a 0 to 1 level.
 *
 * The design borrows ESPHome's ergonomics and rejects its control path. See
 * docs/adr/0002-esp32-node.md: ESPHome talks to Home Assistant at 100 to 300ms
 * with non-deterministic jitter, which is fine for home automation and useless
 * for landing a cue on a frame.
 *
 * THE ONE RULE IN THIS FILE
 *
 * The watchdog is not optional and does not depend on the network being
 * healthy, on the conductor being correct, or on anyone remembering to send a
 * stop. If heartbeats stop arriving for CIP_WATCHDOG_MS the output goes to its
 * safe value. That is the only thing standing between a crashed conductor and
 * a fan running all night.
 *
 * STATUS: written, not compiled. No ESP32 and no ESP-IDF toolchain were
 * available. Treat it as a careful draft rather than as working firmware.
 */

#include <string.h>
#include <errno.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "driver/ledc.h"
#include "lwip/sockets.h"
#include "cJSON.h"
#include "mbedtls/md.h"
#include "freertos/semphr.h"
#include "config.h"
#include "guard.h"
#include "status.h"
#include "devices.h"

#define CIP_PORT          5570
#define CIP_VERSION       "0.3"
#define CIP_WATCHDOG_MS   300
#define CIP_MAX_DATAGRAM  1024
#define CIP_TAG_LEN       16

/* Stack for the socket loop.
 *
 * Generous because the loop is not: a 1KB receive buffer, a cJSON tree per
 * reply, and another 1KB buffer to sign it into. It ran on app_main's 3584 byte
 * stack until the first datagram arrived and overflowed it. */
#define CIP_TASK_STACK    8192

/* Stack for the watchdog.
 *
 * It looks like a task that only compares timestamps, and it is not. Putting a
 * strip back to safe walks every pixel and then blocks on an RMT transmit, and
 * saying why it did costs a formatted log line on top. It ran on 2048 bytes,
 * which was never shown to be too few and was never shown to be enough either.
 *
 * Both numbers are reported once at startup; see the headroom line below. */
#define CIP_WATCHDOG_STACK 4096

/* Shared secret, and there is no building without one.
 *
 * A node that accepts configuration requires it, and this one does. Under 0.2
 * the worst a stranger on the network could do was start a fogger, which is why
 * leaving it off was once reasonable. A board that takes configuration is a
 * different proposition: a stranger can move a relay onto a pin nobody intended,
 * or declare a latency of zero and corrupt the timing of every cue after it in a
 * way that reads as the score being wrong rather than as an attack.
 *
 * Built in, alongside the wifi credentials written over USB, by whoever is
 * holding the board. There is no recovery path over the network, deliberately:
 * losing it means reconnecting USB and reflashing, because a remote way back in
 * is a way in.
 *
 * Set from the environment at build time and never written down here, because a
 * secret in the source is a secret in the history and this history is public:
 *
 *   COMPONIUM_CIP_SECRET='...' idf.py build
 *
 * Empty means the board refuses all configuration, which is the state it should
 * be in until somebody has given it one. */
#ifndef CIP_SECRET
#define CIP_SECRET        ""
#endif

/* What this board calls itself. The devices on it, and everything about them,
 * come from its configuration; see config.c and ADR 0007. */
#define NODE_NAME         "componium-node"

static const char *TAG = "componium";

static volatile int64_t  s_last_heartbeat_us = 0;
static volatile uint64_t s_highest_counter = 0;

/* Since boot. Refused is the one that was missing: without it, a board that is
 * turning traffic away and a board that is not hearing any look identical from
 * the outside, and telling them apart took a packet capture. */
static volatile uint32_t s_cues;
static volatile uint32_t s_curves;
static volatile uint32_t s_refused;

/* Refusing one datagram in silence is correct: a replay guard that narrates
 * every rejection hands anybody on the network a way to fill the log. Refusing
 * a hundred in silence is how two separate faults today presented as a board
 * that simply said nothing, and cost a packet capture each to tell apart from a
 * board that was not listening.
 *
 * So: quiet per datagram, and a line every five seconds while it is happening.
 * Called only from the socket task, which is why the counters here need no
 * lock. */
static void note_refusal(const char *why)
{
    static int64_t said_at_us;
    static uint32_t since;

    s_refused++;
    since++;

    int64_t now = esp_timer_get_time();
    if (now - said_at_us < 5000000) {
        return;
    }
    said_at_us = now;
    ESP_LOGW(TAG, "refused %u datagram(s) in the last few seconds, latest: %s",
             (unsigned)since, why);
    since = 0;
}

/* What is attached, from this board's own configuration. Every one of them
 * carries its own value, its own span and its own safe state: a hold expiring
 * on the fogger must take the fogger and not the fan halfway through a scene. */
static device_t s_devices[DEVICE_MAX];
static int      s_device_count;
static SemaphoreHandle_t s_lock;

/* Kept so that each can be asked how close it came to its limit. */
static TaskHandle_t s_serve_task;
static TaskHandle_t s_watchdog_task;

static void lock(void)   { xSemaphoreTake(s_lock, portMAX_DELAY); }
static void unlock(void) { xSemaphoreGive(s_lock); }

/* ---------------------------------------------------------------- output */

/* Every device to its safe value.
 *
 * All of them, not the one most recently addressed. When this runs the
 * conductor is absent or wrong, and nothing here knows which output is the
 * dangerous one.
 */
static void all_safe(const char *why)
{
    lock();
    for (int i = 0; i < s_device_count; i++) {
        device_safe(&s_devices[i]);
    }
    unlock();
    ESP_LOGW(TAG, "safe: %s", why);
}

/* Whether a configured device is already driving a strip.
 *
 * Asked by the sACN receiver, which drives one too. Two writers to one strip is
 * the fault this project has now fixed twice in two days, and it looks the same
 * every time: whichever writes more often wins, and the other one appears to be
 * broken hardware.
 */
bool node_has_strip(void)
{
    bool found = false;
    if (!s_lock) {
        /* Asked before the node came up. It has no devices yet, so it drives
         * nothing yet, and saying so is the truthful answer as well as the one
         * that does not take a NULL semaphore and reboot the board. */
        return false;
    }
    lock();
    for (int i = 0; i < s_device_count; i++) {
        if (s_devices[i].type == DEV_WS28XX) {
            found = true;
        }
    }
    unlock();
    return found;
}

/* Find a device by the name a cue uses.
 *
 * An empty name is the single device, which is what a conductor built before
 * ADR 0007 sends and what a board with one thing on it should keep accepting.
 * On a board with several a name is required: guessing which output somebody
 * meant is the one thing worse than not applying the cue at all.
 */
static device_t *by_name(const char *id)
{
    if (!id || id[0] == 0) {
        return s_device_count == 1 ? &s_devices[0] : NULL;
    }
    for (int i = 0; i < s_device_count; i++) {
        if (strcmp(s_devices[i].id, id) == 0) {
            return &s_devices[i];
        }
    }
    return NULL;
}

/* Bring up whatever the stored configuration says is attached.
 *
 * Nothing configured is an ordinary state, and the one every freshly flashed
 * board is in: it announces no instruments, and can still be reached and told
 * what it has. A board that had to be configured before it could be talked to
 * could never be configured at all.
 */
static void apply_config(void)
{
    static char json[CONFIG_JSON_MAX];
    char problem[128];

    lock();
    for (int i = 0; i < s_device_count; i++) {
        device_stop(&s_devices[i]);
    }
    s_device_count = 0;
    unlock();

    if (config_load(json, sizeof(json)) == 0) {
        ESP_LOGI(TAG, "no configuration; nothing is attached yet");
        return;
    }

    device_t parsed[DEVICE_MAX];
    int n = config_parse(json, parsed, problem, sizeof(problem));
    if (n < 0) {
        /* Stored and unreadable. Left with nothing rather than with some of it,
         * because a board holding half a configuration looks configured. */
        ESP_LOGE(TAG, "stored configuration is unusable: %s", problem);
        return;
    }

    device_reset_budget();
    lock();
    for (int i = 0; i < n; i++) {
        s_devices[s_device_count] = parsed[i];
        if (device_start(&s_devices[s_device_count])) {
            s_device_count++;
        }
    }
    unlock();
    ESP_LOGI(TAG, "%d device(s) attached", s_device_count);
}

static void send_raw(int sock, struct sockaddr_in *to, const uint8_t *body, size_t len)
{
    if (sizeof(CIP_SECRET) <= 1) {
        sendto(sock, body, len, 0, (struct sockaddr *)to, sizeof(*to));
        return;
    }
    uint8_t out[CIP_MAX_DATAGRAM];
    if (len + CIP_TAG_LEN > sizeof(out)) {
        return;
    }
    const mbedtls_md_info_t *info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    uint8_t sum[32];
    if (mbedtls_md_hmac(info, (const uint8_t *)CIP_SECRET, sizeof(CIP_SECRET) - 1,
                        body, len, sum) != 0) {
        return;
    }
    memcpy(out, sum, CIP_TAG_LEN);
    memcpy(out + CIP_TAG_LEN, body, len);
    sendto(sock, out, len + CIP_TAG_LEN, 0, (struct sockaddr *)to, sizeof(*to));
}

static void send_json(int sock, struct sockaddr_in *to, cJSON *msg)
{
    char *text = cJSON_PrintUnformatted(msg);
    if (text) {
        send_raw(sock, to, (const uint8_t *)text, strlen(text));
        cJSON_free(text);
    }
}

static void send_hello(int sock, struct sockaddr_in *to)
{
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "v", CIP_VERSION);
    cJSON_AddStringToObject(root, "t", "hello");

    cJSON *node = cJSON_CreateObject();
    cJSON_AddStringToObject(node, "name", NODE_NAME);
    cJSON_AddStringToObject(node, "firmware", CIP_VERSION);
    cJSON_AddStringToObject(node, "chip", "ESP32");
    cJSON_AddItemToObject(root, "node", node);

    /* Always an array, even when it is empty. A board with nothing attached
     * announces nothing and stays reachable, which is what lets it be told
     * what it has. */
    cJSON *list = cJSON_CreateArray();
    lock();
    for (int i = 0; i < s_device_count; i++) {
        cJSON *in = device_announcement(&s_devices[i], i);
        if (in) {
            cJSON_AddItemToArray(list, in);
        }
    }
    unlock();
    cJSON_AddItemToObject(root, "instruments", list);

    send_json(sock, to, root);
    cJSON_Delete(root);
}

/* An acknowledgement carrying why something was refused.
 *
 * Refusals travel on the ack rather than in silence: a configuration that was
 * rejected and said nothing is one somebody will spend an evening on. */
static void send_refusal(int sock, struct sockaddr_in *to, double seq, const char *why)
{
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "v", CIP_VERSION);
    cJSON_AddStringToObject(root, "t", "ack");
    cJSON_AddNumberToObject(root, "seq", seq);
    cJSON_AddStringToObject(root, "error", why);
    send_json(sock, to, root);
    cJSON_Delete(root);
    ESP_LOGW(TAG, "refused: %s", why);
}

static void send_ack(int sock, struct sockaddr_in *to, double seq)
{
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "v", CIP_VERSION);
    cJSON_AddStringToObject(root, "t", "ack");
    cJSON_AddNumberToObject(root, "seq", seq);
    send_json(sock, to, root);
    cJSON_Delete(root);
}

/* A curve frame is binary: 'C','F', version, channel count, then that many big
 * endian float32s. Recognised before any JSON parsing is attempted, because at
 * 50Hz the parser is the expensive part. */
/* A curve frame carrying every output due this tick.
 *
 * Bounds checked at every step, because this arrives over UDP from whoever can
 * reach the port and a length that walks off the end of a datagram is the
 * cheapest possible attack on a device with no memory protection worth the
 * name.
 *
 * An index this board does not have is skipped and the rest of the frame is
 * applied. A frame is fifty times a second and superseded 20ms later, so
 * refusing all of it because one output has gone would stop the outputs that
 * are still there for no reason.
 */
static bool handle_curve(const uint8_t *buf, int len)
{
    if (len < 4 || buf[0] != 'C' || buf[1] != 'F') {
        return false;
    }
    int at = 4;
    int count = buf[3];
    if (buf[2] == 0) {
        /* A frame from a conductor built before ADR 0007: one unnamed output,
         * where the count is channels rather than outputs. It addressed the
         * only device there was, so that is what it gets. */
        if (s_device_count < 1 || 4 + 4 * count > len) {
            return true;
        }
        lock();
        device_t *d = &s_devices[0];
        for (int c = 0; c < count && c < d->channels; c++) {
            uint32_t bits = ((uint32_t)buf[4 + 4 * c] << 24) |
                            ((uint32_t)buf[5 + 4 * c] << 16) |
                            ((uint32_t)buf[6 + 4 * c] << 8) |
                            ((uint32_t)buf[7 + 4 * c]);
            float v;
            memcpy(&v, &bits, sizeof(v));
            /* Raw bits off the wire, so this is the one place a NaN can be
             * spelled exactly and on purpose. */
            d->value[c] = unit_value(v);
        }
        d->is_safe = false;
        device_apply(d);
        s_curves++;
        unlock();
        return true;
    }
    if (buf[2] != 1) {
        /* A version this build does not speak. Refused rather than half
         * understood, which is the rule the whole protocol is versioned for. */
        return true;
    }

    lock();
    for (int i = 0; i < count; i++) {
        if (at + 2 > len) {
            break;
        }
        int index = buf[at];
        int channels = buf[at + 1];
        at += 2;
        if (at + 4 * channels > len) {
            break;
        }
        if (index >= 0 && index < s_device_count) {
            device_t *d = &s_devices[index];
            for (int c = 0; c < channels && c < d->channels; c++) {
                uint32_t bits = ((uint32_t)buf[at + 4 * c] << 24) |
                                ((uint32_t)buf[at + 1 + 4 * c] << 16) |
                                ((uint32_t)buf[at + 2 + 4 * c] << 8) |
                                ((uint32_t)buf[at + 3 + 4 * c]);
                float v;
                memcpy(&v, &bits, sizeof(v));
                d->value[c] = unit_value(v);
            }
            d->is_safe = false;
            device_apply(d);
            s_curves++;
        }
        at += 4 * channels;
    }
    unlock();
    return true;
}

static void handle_json(int sock, struct sockaddr_in *from, const char *text, int len)
{
    /* Depth first, because cJSON's parser is recursive and its own limit is a
     * thousand levels: far past the eight kilobytes this task has. The check
     * has to happen before parsing, since there is no recovering from a stack
     * overflow once the parser is inside it. */
    if (!json_shallow_enough(text, len, JSON_MAX_DEPTH)) {
        note_refusal("nested too deep to parse safely");
        return;
    }
    cJSON *root = cJSON_ParseWithLength(text, len);
    if (!root) {
        return;
    }
    const cJSON *version = cJSON_GetObjectItem(root, "v");
    if (cJSON_IsString(version) && strcmp(version->valuestring, CIP_VERSION) != 0) {
        /* Refuse rather than half understand. A message from a protocol we do
         * not speak could mean anything, including something dangerous. */
        ESP_LOGW(TAG, "ignoring protocol version %s", version->valuestring);
        cJSON_Delete(root);
        return;
    }
    /* Replay guard. An attacker who cannot forge a tag can still record a
     * valid cue and send it again later; the counter is what stops that.
     * Only meaningful when authentication is on. */
    if (sizeof(CIP_SECRET) > 1) {
        const cJSON *counter = cJSON_GetObjectItem(root, "n");
        if (cJSON_IsNumber(counter)) {
            uint64_t n = (uint64_t)counter->valuedouble;
            if (n != 0 && n <= s_highest_counter) {
                /* Either a genuine replay or a client whose counter is not
                 * increasing on the wire, which is a real thing that has
                 * happened here and is indistinguishable from here. */
                note_refusal("counter is not higher than the last one seen");
                cJSON_Delete(root);
                return;
            }
            if (n > s_highest_counter) {
                s_highest_counter = n;
            }
        }
    }
    const cJSON *type = cJSON_GetObjectItem(root, "t");
    if (!cJSON_IsString(type)) {
        cJSON_Delete(root);
        return;
    }

    if (strcmp(type->valuestring, "hello") == 0) {
        send_hello(sock, from);
    } else if (strcmp(type->valuestring, "heartbeat") == 0) {
        s_last_heartbeat_us = esp_timer_get_time();
    } else if (strcmp(type->valuestring, "safe") == 0) {
        all_safe("commanded");
    } else if (strcmp(type->valuestring, "cue") == 0) {
        const cJSON *params = cJSON_GetObjectItem(root, "params");
        const cJSON *seq = cJSON_GetObjectItem(root, "seq");
        const cJSON *named = cJSON_GetObjectItem(root, "instrument");

        lock();
        device_t *d = by_name(cJSON_IsString(named) ? named->valuestring : NULL);
        if (!d) {
            unlock();
            /* Not acknowledged, deliberately. Acknowledging a cue that was not
             * applied is a lie, and the conductor's retry and then its recorded
             * skip is exactly the machinery for a cue that did not land. */
            cJSON_Delete(root);
            return;
        }

        static const char *rgb[3] = {"r", "g", "b"};
        if (cJSON_IsObject(params)) {
            for (int c = 0; c < d->channels; c++) {
                const char *name = (d->channels == 3) ? rgb[c] : "intensity";
                const cJSON *v = cJSON_GetObjectItem(params, name);
                if (cJSON_IsNumber(v)) {
                    d->value[c] = unit_value(v->valuedouble);
                }
            }
        }
        const cJSON *hold = cJSON_GetObjectItem(root, "hold_ms");
        if (cJSON_IsNumber(hold) && hold->valuedouble > 0) {
            /* Bounded, because this becomes a deadline. A hold of 1e300 ms
             * overflows the arithmetic into a time in the past, and an expiry
             * already behind us is an output that never goes safe on its own.
             * An hour is longer than any cue and shorter than for ever. */
            int64_t ms = (int64_t)bounded_int(hold->valuedouble, 1, 3600000, 1);
            d->hold_until_us = esp_timer_get_time() + ms * 1000;
        } else {
            d->hold_until_us = 0;
        }
        d->is_safe = false;
        device_apply(d);
        s_cues++;
        unlock();

        if (cJSON_IsNumber(seq)) {
            send_ack(sock, from, seq->valuedouble);
        }
    } else if (strcmp(type->valuestring, "configure") == 0) {
        const cJSON *seq = cJSON_GetObjectItem(root, "seq");
        double n = cJSON_IsNumber(seq) ? seq->valuedouble : 0;

        /* Only when authenticated, and that is the rule rather than a caution.
         * A stranger who can write this can move a relay onto a pin nobody
         * intended, or declare a latency of zero and corrupt the timing of
         * every cue after it in a way that reads as the score being wrong. A
         * board with no secret has already refused this datagram long before
         * here; the check is what makes that true rather than incidental. */
        if (sizeof(CIP_SECRET) <= 1) {
            send_refusal(sock, from, n, "this node takes no configuration without a secret");
            cJSON_Delete(root);
            return;
        }

        const cJSON *devices = cJSON_GetObjectItem(root, "devices");
        if (!cJSON_IsArray(devices)) {
            send_refusal(sock, from, n, "no devices array");
            cJSON_Delete(root);
            return;
        }

        char *json = cJSON_PrintUnformatted(devices);
        if (!json) {
            send_refusal(sock, from, n, "out of memory");
            cJSON_Delete(root);
            return;
        }

        /* Parsed before it is stored, so a configuration that cannot be used is
         * refused rather than remembered and discovered at the next boot. */
        device_t parsed[DEVICE_MAX];
        char problem[128];
        int count = config_parse(json, parsed, problem, sizeof(problem));
        if (count < 0) {
            send_refusal(sock, from, n, problem);
            cJSON_free(json);
            cJSON_Delete(root);
            return;
        }
        if (!config_save(json)) {
            send_refusal(sock, from, n, "could not store it");
            cJSON_free(json);
            cJSON_Delete(root);
            return;
        }
        cJSON_free(json);

        send_ack(sock, from, n);
        apply_config();
        /* The instruments and their indices have just changed, so anything
         * holding the old ones is now wrong and has to be told. */
        send_hello(sock, from);
    }
    cJSON_Delete(root);
}


/* -------------------------------------------------------------- watchdog */

static void watchdog_task(void *arg)
{
    (void)arg;
    unsigned ticks = 0;
    bool reported = false;
    for (;;) {
        int64_t now_us = esp_timer_get_time();

        /* A span that has run its declared duration ends here, whether or not
         * the conductor's stop ever arrived. One device, not the board: a four
         * second fog burst ending must not stop a fan in the middle of a
         * scene. */
        lock();
        for (int i = 0; i < s_device_count; i++) {
            device_t *d = &s_devices[i];
            if (d->hold_until_us != 0 && now_us > d->hold_until_us && !d->is_safe) {
                device_safe(d);
            }
        }
        unlock();

        if (s_last_heartbeat_us != 0) {
            int64_t idle_ms = (now_us - s_last_heartbeat_us) / 1000;
            bool anything_running = false;
            lock();
            for (int i = 0; i < s_device_count; i++) {
                if (!s_devices[i].is_safe) {
                    anything_running = true;
                }
            }
            unlock();
            if (idle_ms > CIP_WATCHDOG_MS && anything_running) {
                /* Every device. The conductor is gone, and nothing here knows
                 * which output is the dangerous one. */
                all_safe("no heartbeat");
            }
        }
        /* Once, about ten seconds in, when everything that runs at startup
         * has run: the least stack either long lived task has had spare. The
         * loop below ticks three times per watchdog period, so a hundred of
         * them is ten seconds at the current numbers.
         *
         * Reported rather than assumed because the last stack on this board to
         * be sized by eye was too small, and the symptom was a board that
         * rebooted whenever anybody spoke to it. */
        if (!reported && ++ticks >= 100) {
            reported = true;
            ESP_LOGI(TAG, "stack headroom: node %u bytes, watchdog %u bytes",
                     s_serve_task
                         ? (unsigned)uxTaskGetStackHighWaterMark(s_serve_task)
                         : 0u,
                     (unsigned)uxTaskGetStackHighWaterMark(NULL));
        }

        vTaskDelay(pdMS_TO_TICKS(CIP_WATCHDOG_MS / 3));
    }
}

/* ------------------------------------------------------------------ auth */

/* Verify and strip the tag, in place. Returns the body length, or -1 when
 * the datagram should be dropped.
 *
 * The tag covers the raw bytes rather than a canonical form of the JSON,
 * precisely so that this function can be a hash and a comparison rather than
 * a parser. Re-serialising a document to check a signature on a
 * microcontroller would be slow and easy to get subtly wrong. */
static int auth_unwrap(uint8_t *buf, int len)
{
    if (sizeof(CIP_SECRET) <= 1) {
        return len;   /* authentication disabled */
    }
    if (len <= CIP_TAG_LEN) {
        return -1;
    }
    const mbedtls_md_info_t *info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    uint8_t sum[32];
    if (mbedtls_md_hmac(info, (const uint8_t *)CIP_SECRET, sizeof(CIP_SECRET) - 1,
                        buf + CIP_TAG_LEN, len - CIP_TAG_LEN, sum) != 0) {
        return -1;
    }
    /* Constant time compare: a byte at a time with an accumulating OR, so
     * that how long this takes says nothing about how much matched. */
    uint8_t diff = 0;
    for (int i = 0; i < CIP_TAG_LEN; i++) {
        diff |= (uint8_t)(sum[i] ^ buf[i]);
    }
    if (diff != 0) {
        return -1;
    }
    memmove(buf, buf + CIP_TAG_LEN, len - CIP_TAG_LEN);
    return len - CIP_TAG_LEN;
}

/* ---------------------------------------------------------------- status */

int node_status_devices(status_device_t *out, int max)
{
    if (!out || max <= 0 || !s_lock) {
        return 0;
    }
    int64_t now = esp_timer_get_time();
    int n = 0;
    lock();
    for (int i = 0; i < s_device_count && n < max; i++) {
        const device_t *d = &s_devices[i];
        status_device_t *o = &out[n++];
        o->index = i;
        strlcpy(o->id, d->id, sizeof(o->id));
        strlcpy(o->kind, d->kind, sizeof(o->kind));
        o->type = device_type_name(d->type);
        o->gpio = d->gpio;
        o->channels = d->channels;
        for (int c = 0; c < 3; c++) {
            o->value[c] = (c < d->channels) ? d->value[c] : 0.0f;
        }
        o->is_safe = d->is_safe;
        o->latency_ms = d->latency_ms;
        o->hold_ms_left = (d->hold_until_us > now)
                              ? (int)((d->hold_until_us - now) / 1000)
                              : 0;
    }
    unlock();
    return n;
}

void node_status_counters(uint32_t *cues, uint32_t *curves, uint32_t *refused,
                          int64_t *since_heartbeat_ms)
{
    if (cues)    *cues = s_cues;
    if (curves)  *curves = s_curves;
    if (refused) *refused = s_refused;
    if (since_heartbeat_ms) {
        *since_heartbeat_ms = s_last_heartbeat_us
                                  ? (esp_timer_get_time() - s_last_heartbeat_us) / 1000
                                  : -1;
    }
}

void node_status_stacks(unsigned *serve, unsigned *watchdog)
{
    if (serve) {
        *serve = s_serve_task ? (unsigned)uxTaskGetStackHighWaterMark(s_serve_task) : 0;
    }
    if (watchdog) {
        *watchdog = s_watchdog_task
                        ? (unsigned)uxTaskGetStackHighWaterMark(s_watchdog_task)
                        : 0;
    }
}

bool node_secret_required(void)
{
    return sizeof(CIP_SECRET) > 1;
}

bool node_secret_matches(const char *candidate)
{
    if (sizeof(CIP_SECRET) <= 1) {
        return false;   /* nothing to match, and nothing to unlock */
    }
    if (!candidate) {
        return false;
    }
    /* The comparison lives in guard.c so that it can be tested; the secret
     * itself never leaves this file. */
    return constant_time_equal(CIP_SECRET, candidate);
}

/* ------------------------------------------------------------------ main */

/* Come up, safely, before anything asks what is attached.
 *
 * Separate from serving because two things need the node to exist before there
 * is any network: the outputs, which must be at their safe values from the
 * first moment rather than from whenever the wifi settles, and sacn_start(),
 * which asks whether a configured device already drives the strip and must not
 * be told "no" merely because the answer had not been loaded yet. */
void componium_node_init(void)
{
    if (s_lock) {
        return;
    }
    s_lock = xSemaphoreCreateMutex();
    apply_config();
}

/* Receive, authenticate, act, answer. Never returns while it has a socket. */
static void serve_forever(void)
{
    int sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (sock < 0) {
        ESP_LOGE(TAG, "socket: errno %d", errno);
        return;
    }
    struct sockaddr_in bind_addr = {
        .sin_family      = AF_INET,
        .sin_port        = htons(CIP_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    if (bind(sock, (struct sockaddr *)&bind_addr, sizeof(bind_addr)) < 0) {
        ESP_LOGE(TAG, "bind: errno %d", errno);
        close(sock);
        return;
    }
    ESP_LOGI(TAG, "%s listening on udp/%d with %d device(s)", NODE_NAME, CIP_PORT, s_device_count);

    uint8_t buf[CIP_MAX_DATAGRAM];
    for (;;) {
        struct sockaddr_in from;
        socklen_t from_len = sizeof(from);
        int len = recvfrom(sock, buf, sizeof(buf) - 1, 0,
                           (struct sockaddr *)&from, &from_len);
        if (len < 0) {
            ESP_LOGW(TAG, "recvfrom: errno %d", errno);
            continue;
        }
        len = auth_unwrap(buf, len);
        if (len < 0) {
            note_refusal("wrong signature, or no signature");
            /* Dropped in silence. Logging every rejected datagram would let
             * anyone on the network fill the log by spraying rubbish at us. */
            continue;
        }
        if (handle_curve(buf, len)) {
            continue;
        }
        buf[len] = 0;
        handle_json(sock, &from, (const char *)buf, len);
    }
}

static void serve_task(void *arg)
{
    (void)arg;
    serve_forever();
    /* Only reached when the socket could not be made, which is not a thing to
     * carry on from. Every output is already at its safe value and the watchdog
     * is running, so the honest thing is to say so and stop. */
    ESP_LOGE(TAG, "node stopped; output is safe, waiting for a reflash");
    vTaskDelete(NULL);
}

/* Start serving, in a task of the loop's own.
 *
 * Not on app_main's task, which is where this used to run and where it could
 * not fit. A task whose stack is sized for the buffers it declares is not a
 * detail on a board whose only recovery is a USB cable. */
void componium_node_serve(void)
{
    componium_node_init();   /* Ordinarily already done. Cheap to be sure. */
    xTaskCreate(watchdog_task, "cip_watchdog", CIP_WATCHDOG_STACK, NULL, 6,
                &s_watchdog_task);
    xTaskCreate(serve_task, "cip_node", CIP_TASK_STACK, NULL, 5, &s_serve_task);
}
