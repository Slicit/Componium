/* Receiving E1.31, so the strip on this board is an ordinary lighting fixture.
 *
 * The conductor has spoken sACN since M4 and every lighting desk and controller
 * in the world receives it. Inventing a Componium way to send a colour to a
 * strip would be competing with a protocol that already works, which LOGBOOK.md
 * lists as a non-goal in as many words. So the strip is addressed exactly as a
 * WLED controller is, and the only thing that changes in a rig file is the IP.
 *
 * What arrives is one fixture's worth of slots, not a pixel array: the
 * conductor writes 1, 3 or 4 channels from the fixture's start address, which
 * is what a lighting desk sends and what WLED's single colour DMX mode expects.
 * The whole strip takes that colour.
 *
 * THE WATCHDOG HERE IS NOT THE FAN'S WATCHDOG
 *
 * The node's CIP watchdog is 300ms because a fan running all night is the
 * hazard it exists for. A light is not a hazard, and 300ms of ordinary network
 * hiccup would make it flicker. The danger here is the opposite one: a strip
 * left holding a colour for ever after the conductor has gone away. So it holds
 * through a stumble and goes dark after a few seconds of real silence.
 */

#include "sacn.h"
#include "led.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "lwip/sockets.h"

static const char *TAG = "sacn";

#define SACN_PORT 5568

/* Which universe and which channels within it. A lighting desk numbers the
 * start address from 1, and so does this, because arguing with that convention
 * has never once helped anybody. */
#ifndef SACN_UNIVERSE
#define SACN_UNIVERSE 1
#endif
#ifndef SACN_START
#define SACN_START 1
#endif
/* 1 dimmer, 3 rgb, 4 rgbw. Matches `mode` in the rig. */
#ifndef SACN_WIDTH
#define SACN_WIDTH 3
#endif

/* How long a colour survives silence. See the note above: this is not the
 * fan's 300ms and must not be. */
#define HOLD_MS 5000

/* Offsets into an E1.31 packet, which is a fixed layout and worth naming. */
#define OFF_ACN_ID     4     /* 12 bytes: "ASC-E1.17" and three zeros */
#define OFF_PRIORITY   108
#define OFF_SEQUENCE   111
#define OFF_OPTIONS    112
#define OFF_UNIVERSE   113
#define OFF_COUNT      123   /* property values, start code included */
#define OFF_START_CODE 125
#define OFF_SLOTS      126
#define HEADER_BYTES   126

/* Bit 6 of the options byte: this source is finished and the receiver should
 * stop treating its last frame as current. */
#define OPTION_TERMINATED 0x40

static const char ACN_ID[12] = { 'A','S','C','-','E','1','.','1','7', 0, 0, 0 };

static int64_t s_last_us;
static uint8_t s_sequence;
static bool    s_have_sequence;

/**
 * Whether this packet follows the last one.
 *
 * E1.31 numbers frames in a byte that wraps, and says to discard a packet whose
 * distance from the last is between -20 and 0: that is a straggler arriving
 * after a newer frame, and painting it would flicker backwards in time. A jump
 * larger than that is a source that restarted, which is a new sequence rather
 * than a bad one.
 */
static bool in_order(uint8_t seq)
{
    if (!s_have_sequence) {
        s_have_sequence = true;
        s_sequence = seq;
        return true;
    }
    int8_t gap = (int8_t)(seq - s_sequence);
    if (gap <= 0 && gap > -20) {
        return false;
    }
    s_sequence = seq;
    return true;
}

/** Paint from one packet, or say why not. */
static void take(const uint8_t *p, int len)
{
    if (len < HEADER_BYTES + 1) {
        return;
    }
    if (memcmp(p + OFF_ACN_ID, ACN_ID, sizeof(ACN_ID)) != 0) {
        return;   /* not E1.31 at all */
    }
    uint16_t universe = (uint16_t)((p[OFF_UNIVERSE] << 8) | p[OFF_UNIVERSE + 1]);
    if (universe != SACN_UNIVERSE) {
        return;
    }
    if (p[OFF_START_CODE] != 0x00) {
        return;   /* not dimmer data: RDM and friends live here too */
    }
    if (p[OFF_OPTIONS] & OPTION_TERMINATED) {
        ESP_LOGI(TAG, "source finished");
        s_have_sequence = false;
        s_last_us = 0;
        led_off();
        return;
    }
    if (!in_order(p[OFF_SEQUENCE])) {
        return;
    }

    int count = ((p[OFF_COUNT] << 8) | p[OFF_COUNT + 1]) - 1;   /* less the start code */
    int slots = len - OFF_SLOTS;
    if (count < slots) {
        slots = count;
    }
    /* Start addresses are 1 based, so the first slot is at index 0. */
    int from = SACN_START - 1;
    if (from < 0 || from + SACN_WIDTH > slots) {
        return;   /* the fixture is not in this packet */
    }
    const uint8_t *v = p + OFF_SLOTS + from;

    s_last_us = esp_timer_get_time();
#if SACN_WIDTH == 1
    led_paint(v[0], v[0], v[0]);
#else
    led_paint(v[0], v[1], v[2]);
#endif
}

static void listener(void *arg)
{
    (void)arg;
    int sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (sock < 0) {
        ESP_LOGE(TAG, "socket: errno %d", errno);
        vTaskDelete(NULL);
        return;
    }
    int on = 1;
    setsockopt(sock, SOL_SOCKET, SO_REUSEADDR, &on, sizeof(on));

    struct sockaddr_in bind_addr = {
        .sin_family      = AF_INET,
        .sin_port        = htons(SACN_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    if (bind(sock, (struct sockaddr *)&bind_addr, sizeof(bind_addr)) < 0) {
        ESP_LOGE(TAG, "bind: errno %d", errno);
        close(sock);
        vTaskDelete(NULL);
        return;
    }

    /* Unicast is what a rig should use, because consumer access points drop
     * multicast with dispiriting regularity. Joining the group anyway costs
     * nothing and means a desk configured the conventional way is not silently
     * ignored. */
    struct ip_mreq group = {
        .imr_multiaddr.s_addr = htonl(0xEFFF0000 | SACN_UNIVERSE),  /* 239.255.x.x */
        .imr_interface.s_addr = htonl(INADDR_ANY),
    };
    if (setsockopt(sock, IPPROTO_IP, IP_ADD_MEMBERSHIP, &group, sizeof(group)) < 0) {
        ESP_LOGW(TAG, "no multicast for universe %d, unicast still works", SACN_UNIVERSE);
    }

    /* A read that gives up lets the same loop notice silence, so there is no
     * second task and no timer just to turn a light off. */
    struct timeval wait = { .tv_sec = 1, .tv_usec = 0 };
    setsockopt(sock, SOL_SOCKET, SO_RCVTIMEO, &wait, sizeof(wait));

    ESP_LOGI(TAG, "listening on udp/%d, universe %d, address %d, width %d",
             SACN_PORT, SACN_UNIVERSE, SACN_START, SACN_WIDTH);

    uint8_t buf[640];
    for (;;) {
        int len = recvfrom(sock, buf, sizeof(buf), 0, NULL, NULL);
        if (len > 0) {
            take(buf, len);
            continue;
        }
        if (s_last_us && (esp_timer_get_time() - s_last_us) / 1000 > HOLD_MS) {
            ESP_LOGI(TAG, "no lighting data for %dms, going dark", HOLD_MS);
            s_last_us = 0;
            s_have_sequence = false;
            led_off();
        }
    }
}

void sacn_start(void)
{
    if (!led_start()) {
        ESP_LOGW(TAG, "no strip, so not listening");
        return;
    }
    xTaskCreate(listener, "sacn", 4096, NULL, 5, NULL);
}
