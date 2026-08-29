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

#define CIP_PORT          5570
#define CIP_VERSION       "0.2"
#define CIP_WATCHDOG_MS   300
#define CIP_MAX_DATAGRAM  1024
#define CIP_TAG_LEN       16

/* Shared secret. Empty disables authentication, which is only reasonable on a
 * wired network you control. With it set, every datagram carries a 16 byte
 * HMAC-SHA256 prefix and anything that fails verification is dropped in
 * silence: replying would confirm this node exists and is worth attacking. */
#define CIP_SECRET        ""

/* Declared characteristics. These must describe the physical device honestly:
 * the conductor dispatches every cue this far ahead, so a lie here makes the
 * whole rig feel wrong in a way that is hard to diagnose from the room. */
#define NODE_ID           "wind.main"
#define NODE_KIND         "wind"
#define NODE_LATENCY_MS   1200
#define NODE_RAMP_UP_MS   1800
#define NODE_RAMP_DOWN_MS 3000
#define NODE_CHANNEL      "intensity"

/* 25kHz keeps a 4 pin fan quiet. Audible switching noise in a cinema defeats
 * the purpose of the entire project. */
#define PWM_GPIO          18
#define PWM_FREQ_HZ       25000
#define PWM_RESOLUTION    LEDC_TIMER_10_BIT
#define PWM_MAX_DUTY      1023

static const char *TAG = "componium";

static volatile int64_t s_last_heartbeat_us = 0;
static volatile float   s_level = 0.0f;
static volatile bool    s_safe = true;
static volatile uint64_t s_highest_counter = 0;

/* A span ends when its hold expires, whether or not the conductor's stop
 * ever arrives. This is the layer that survives a lost datagram, and it is
 * why a cue carries its own duration rather than relying on being told when
 * to stop. */
static volatile int64_t s_hold_until_us = 0;

/* ---------------------------------------------------------------- output */

static void output_apply(float level)
{
    if (level < 0.0f) level = 0.0f;
    if (level > 1.0f) level = 1.0f;   /* clamp, never wrap */
    uint32_t duty = (uint32_t)(level * PWM_MAX_DUTY + 0.5f);
    ledc_set_duty(LEDC_LOW_SPEED_MODE, LEDC_CHANNEL_0, duty);
    ledc_update_duty(LEDC_LOW_SPEED_MODE, LEDC_CHANNEL_0);
    s_level = level;
}

static void output_safe(const char *why)
{
    if (!s_safe) {
        ESP_LOGW(TAG, "going safe: %s", why);
    }
    s_safe = true;
    s_hold_until_us = 0;
    output_apply(0.0f);
}

static void output_init(void)
{
    ledc_timer_config_t timer = {
        .speed_mode      = LEDC_LOW_SPEED_MODE,
        .duty_resolution = PWM_RESOLUTION,
        .timer_num       = LEDC_TIMER_0,
        .freq_hz         = PWM_FREQ_HZ,
        .clk_cfg         = LEDC_AUTO_CLK,
    };
    ledc_timer_config(&timer);

    ledc_channel_config_t channel = {
        .gpio_num   = PWM_GPIO,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel    = LEDC_CHANNEL_0,
        .timer_sel  = LEDC_TIMER_0,
        .duty       = 0,
        .hpoint     = 0,
    };
    ledc_channel_config(&channel);
    output_safe("boot");
}

/* -------------------------------------------------------------- protocol */

/* Prefix the tag on the way out. A node that verifies inbound traffic but
 * sends its replies unauthenticated would be rejected by its own conductor,
 * which is a confusing way to discover a half finished implementation. */
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
    cJSON *manifest = cJSON_CreateObject();
    cJSON *safe = cJSON_CreateObject();
    cJSON *channels = cJSON_CreateArray();
    cJSON *channel = cJSON_CreateObject();
    cJSON *range = cJSON_CreateArray();

    cJSON_AddStringToObject(root, "v", CIP_VERSION);
    cJSON_AddStringToObject(root, "t", "hello");

    cJSON_AddStringToObject(manifest, "id", NODE_ID);
    cJSON_AddStringToObject(manifest, "kind", NODE_KIND);
    cJSON_AddNumberToObject(manifest, "latency_ms", NODE_LATENCY_MS);
    cJSON_AddNumberToObject(manifest, "ramp_up_ms", NODE_RAMP_UP_MS);
    cJSON_AddNumberToObject(manifest, "ramp_down_ms", NODE_RAMP_DOWN_MS);

    cJSON_AddNumberToObject(safe, NODE_CHANNEL, 0);
    cJSON_AddItemToObject(manifest, "safe_state", safe);

    cJSON_AddStringToObject(channel, "name", NODE_CHANNEL);
    cJSON_AddStringToObject(channel, "unit", "normalised");
    cJSON_AddItemToArray(range, cJSON_CreateNumber(0));
    cJSON_AddItemToArray(range, cJSON_CreateNumber(1));
    cJSON_AddItemToObject(channel, "range", range);
    cJSON_AddItemToArray(channels, channel);
    cJSON_AddItemToObject(manifest, "channels", channels);

    cJSON_AddItemToObject(root, "manifest", manifest);
    send_json(sock, to, root);
    cJSON_Delete(root);
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
static bool handle_curve(const uint8_t *buf, int len)
{
    if (len < 4 || buf[0] != 'C' || buf[1] != 'F') {
        return false;
    }
    int count = buf[3];
    if (len != 4 + 4 * count || count < 1) {
        return true;   /* malformed, but addressed to us: drop it silently */
    }
    uint32_t bits = ((uint32_t)buf[4] << 24) | ((uint32_t)buf[5] << 16) |
                    ((uint32_t)buf[6] << 8)  | (uint32_t)buf[7];
    float value;
    memcpy(&value, &bits, sizeof(value));
    s_safe = false;
    output_apply(value);
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
        output_safe("commanded");
    } else if (strcmp(type->valuestring, "cue") == 0) {
        const cJSON *params = cJSON_GetObjectItem(root, "params");
        const cJSON *seq = cJSON_GetObjectItem(root, "seq");
        if (cJSON_IsObject(params)) {
            const cJSON *value = cJSON_GetObjectItem(params, NODE_CHANNEL);
            if (cJSON_IsNumber(value)) {
                const cJSON *hold = cJSON_GetObjectItem(root, "hold_ms");
                if (cJSON_IsNumber(hold) && hold->valuedouble > 0) {
                    s_hold_until_us = esp_timer_get_time() +
                                      (int64_t)(hold->valuedouble * 1000);
                } else {
                    s_hold_until_us = 0;
                }
                s_safe = false;
                output_apply((float)value->valuedouble);
            }
        }
        if (cJSON_IsNumber(seq)) {
            send_ack(sock, from, seq->valuedouble);
        }
    }
    cJSON_Delete(root);
}


/* -------------------------------------------------------------- watchdog */

static void watchdog_task(void *arg)
{
    (void)arg;
    for (;;) {
        int64_t now_us = esp_timer_get_time();
        if (s_hold_until_us != 0 && now_us > s_hold_until_us && !s_safe) {
            s_hold_until_us = 0;
            output_safe("hold expired");
        }
        if (s_last_heartbeat_us != 0) {
            int64_t idle_ms = (esp_timer_get_time() - s_last_heartbeat_us) / 1000;
            if (idle_ms > CIP_WATCHDOG_MS && !s_safe) {
                output_safe("no heartbeat");
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
    output_init();
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
    ESP_LOGI(TAG, "%s listening on udp/%d", NODE_ID, CIP_PORT);

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
