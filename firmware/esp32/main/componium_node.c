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
#include "devices.h"

#define CIP_PORT          5570
#define CIP_VERSION       "0.3"
#define CIP_WATCHDOG_MS   300
#define CIP_MAX_DATAGRAM  1024
#define CIP_TAG_LEN       16

/* Shared secret, and there is no building without one.
 *
 * A node that accepts configuration requires it, and this one does. Under 0.2
 * the worst a stranger on the network could do was start a fogger, which is why
 * leaving it off was once reasonable. A board that takes configuration is a
 * different proposition: a stranger can move a relay onto a pin nobody intended,
 * or declare a latency of zero and corrupt the timing of every cue after it in a
 * way that reads as the score being wrong rather than as an attack.
 *
 * Written over USB with the wifi credentials, by whoever is holding the board.
 * There is no recovery path over the network, deliberately: losing it means
 * reconnecting USB and reflashing, because a remote way back in is a way in.
 *
 * Empty here means the board refuses everything, which is the state it should
 * be in until somebody has given it one. */
#define CIP_SECRET        ""

/* What this board calls itself. The devices on it, and everything about them,
 * come from its configuration; see config.c and ADR 0007. */
#define NODE_NAME         "componium-node"

static const char *TAG = "componium";

static volatile int64_t  s_last_heartbeat_us = 0;
static volatile uint64_t s_highest_counter = 0;

/* What is attached, from this board's own configuration. Every one of them
 * carries its own value, its own span and its own safe state: a hold expiring
 * on the fogger must take the fogger and not the fan halfway through a scene. */
static device_t s_devices[DEVICE_MAX];
static int      s_device_count;
static SemaphoreHandle_t s_lock;

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
        const device_t *d = &s_devices[i];
        cJSON *in = cJSON_CreateObject();
        cJSON_AddNumberToObject(in, "index", i);
        cJSON_AddStringToObject(in, "id", d->id);
        cJSON_AddStringToObject(in, "kind", d->kind);
        cJSON_AddNumberToObject(in, "latency_ms", d->latency_ms);
        if (d->ramp_up_ms > 0) {
            cJSON_AddNumberToObject(in, "ramp_up_ms", d->ramp_up_ms);
        }
        if (d->ramp_down_ms > 0) {
            cJSON_AddNumberToObject(in, "ramp_down_ms", d->ramp_down_ms);
        }

        cJSON *safe = cJSON_CreateObject();
        cJSON *channels = cJSON_CreateArray();
        static const char *rgb[3] = {"r", "g", "b"};
        for (int c = 0; c < d->channels; c++) {
            const char *name = (d->channels == 3) ? rgb[c] : "intensity";
            cJSON_AddNumberToObject(safe, name, (d->channels == 3) ? 0 : d->safe);
            cJSON *ch = cJSON_CreateObject();
            cJSON_AddStringToObject(ch, "name", name);
            cJSON_AddStringToObject(ch, "unit", "normalised");
            cJSON *range = cJSON_CreateArray();
            cJSON_AddItemToArray(range, cJSON_CreateNumber(0));
            cJSON_AddItemToArray(range, cJSON_CreateNumber(1));
            cJSON_AddItemToObject(ch, "range", range);
            cJSON_AddItemToArray(channels, ch);
        }
        cJSON_AddItemToObject(in, "safe_state", safe);
        cJSON_AddItemToObject(in, "channels", channels);
        cJSON_AddItemToArray(list, in);
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
            d->value[c] = v;
        }
        d->is_safe = false;
        device_apply(d);
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
                d->value[c] = v;
            }
            d->is_safe = false;
            device_apply(d);
        }
        at += 4 * channels;
    }
    unlock();
    return true;
}

static void handle_json(int sock, struct sockaddr_in *from, const char *text, int len)
{
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
                    d->value[c] = (float)v->valuedouble;
                }
            }
        }
        const cJSON *hold = cJSON_GetObjectItem(root, "hold_ms");
        if (cJSON_IsNumber(hold) && hold->valuedouble > 0) {
            d->hold_until_us = esp_timer_get_time() +
                               (int64_t)(hold->valuedouble * 1000);
        } else {
            d->hold_until_us = 0;
        }
        d->is_safe = false;
        device_apply(d);
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
            free(json);
            cJSON_Delete(root);
            return;
        }
        if (!config_save(json)) {
            send_refusal(sock, from, n, "could not store it");
            free(json);
            cJSON_Delete(root);
            return;
        }
        free(json);

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

/* ------------------------------------------------------------------ main */

void componium_node_start(void)
{
    s_lock = xSemaphoreCreateMutex();
    apply_config();
    xTaskCreate(watchdog_task, "cip_watchdog", 2048, NULL, 6, NULL);

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
