#include "devices.h"

#include <string.h>

#include "cJSON.h"
#include "esp_log.h"
#include "soc/soc_caps.h"

static const char *TAG = "devices";

/* LEDC resolution. Ten bits is 1024 steps, which is more than any fan or lamp
 * resolves and leaves the timer free to run at 25kHz. */
#define PWM_RESOLUTION LEDC_TIMER_10_BIT
#define PWM_MAX_DUTY   1023
#define PWM_TIMER      LEDC_TIMER_0

/* How many of each the chip has. Read from the chip's own headers rather than
 * remembered, because a wrong number here is a device that silently does not
 * work rather than a compile error. */
#define MAX_PWM    SOC_LEDC_CHANNEL_NUM
#define MAX_STRIPS SOC_RMT_TX_CANDIDATES_PER_GROUP

static int used_ledc;
static int used_rmt;

const char *device_pin_problem(int gpio)
{
    if (gpio < 0 || gpio > 39) {
        return "not a pin on this chip";
    }
    /* Input only. From SOC_GPIO_VALID_OUTPUT_GPIO_MASK, which is the chip
     * saying so rather than anybody remembering. */
    if (!((SOC_GPIO_VALID_OUTPUT_GPIO_MASK >> gpio) & 1ULL)) {
        return "input only, it cannot drive anything";
    }
    /* The chip calls these valid and they are wired to the SPI flash. Using one
     * does not fail, it stops the board running. */
    if (gpio >= 6 && gpio <= 11) {
        return "wired to the flash; the board will not run";
    }
    /* The console, where wifi provisioning lives. Taking it removes the only
     * way back into a board that cannot join a network. */
    if (gpio == 1 || gpio == 3) {
        return "the console UART, which is how this board is provisioned";
    }
    /* Strapping pins. Usable with care and not by accident: 12 held high at
     * boot sets the flash voltage and can leave a board that will not start,
     * and the only recovery from that is USB. */
    if (gpio == 0 || gpio == 2 || gpio == 12 || gpio == 15) {
        return "a strapping pin, read at boot; a device here can stop the board starting";
    }
    return NULL;
}

device_type_t device_type_of(const char *name)
{
    if (!name) {
        return DEV_NONE;
    }
    if (strcmp(name, "pwm") == 0) {
        return DEV_PWM;
    }
    if (strcmp(name, "ws28xx") == 0) {
        return DEV_WS28XX;
    }
    if (strcmp(name, "relay") == 0) {
        return DEV_RELAY;
    }
    return DEV_NONE;
}

/* Every device starts over: the counters are how many of a finite peripheral
 * this configuration has claimed, not how many have ever been claimed. */
void device_reset_budget(void)
{
    used_ledc = 0;
    used_rmt = 0;
}

static bool start_pwm(device_t *d)
{
    if (used_ledc >= MAX_PWM) {
        ESP_LOGE(TAG, "%s: no LEDC channels left, this chip has %d", d->id, MAX_PWM);
        return false;
    }
    int freq = d->freq_hz > 0 ? d->freq_hz : 25000;

    /* One timer shared by every dimmed output, which is why they share a
     * frequency. 25kHz is above hearing and is what a four pin fan expects;
     * a build wanting two frequencies would need a second timer, and there
     * are four. */
    static bool timer_ready;
    if (!timer_ready) {
        ledc_timer_config_t timer = {
            .speed_mode      = LEDC_LOW_SPEED_MODE,
            .duty_resolution = PWM_RESOLUTION,
            .timer_num       = PWM_TIMER,
            .freq_hz         = freq,
            .clk_cfg         = LEDC_AUTO_CLK,
        };
        if (ledc_timer_config(&timer) != ESP_OK) {
            ESP_LOGE(TAG, "%s: no timer at %dHz", d->id, freq);
            return false;
        }
        timer_ready = true;
    }

    d->ledc = (ledc_channel_t)used_ledc++;
    ledc_channel_config_t channel = {
        .gpio_num   = d->gpio,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel    = d->ledc,
        .timer_sel  = PWM_TIMER,
        .duty       = 0,
        .hpoint     = 0,
    };
    return ledc_channel_config(&channel) == ESP_OK;
}

static bool start_strip(device_t *d)
{
    if (used_rmt >= MAX_STRIPS) {
        ESP_LOGE(TAG, "%s: no RMT channels left, this chip has %d", d->id, MAX_STRIPS);
        return false;
    }
    led_strip_config_t strip = {
        .strip_gpio_num   = d->gpio,
        .max_leds         = d->pixels > 0 ? d->pixels : 30,
        .led_pixel_format = LED_PIXEL_FORMAT_GRB,
        .led_model        = LED_MODEL_WS2812,
        .flags.invert_out = false,
    };
    led_strip_rmt_config_t rmt = {
        .clk_src        = RMT_CLK_SRC_DEFAULT,
        .resolution_hz  = 10 * 1000 * 1000,
        .flags.with_dma = false,
    };
    if (led_strip_new_rmt_device(&strip, &rmt, &d->strip) != ESP_OK) {
        return false;
    }
    used_rmt++;
    return true;
}

static bool start_relay(device_t *d)
{
    gpio_config_t pin = {
        .pin_bit_mask = 1ULL << d->gpio,
        .mode         = GPIO_MODE_OUTPUT,
        .pull_up_en   = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type    = GPIO_INTR_DISABLE,
    };
    return gpio_config(&pin) == ESP_OK;
}

bool device_start(device_t *d)
{
    const char *why = device_pin_problem(d->gpio);
    if (why) {
        ESP_LOGE(TAG, "%s: gpio %d is %s", d->id, d->gpio, why);
        return false;
    }

    d->channels = (d->type == DEV_WS28XX) ? 3 : 1;
    bool ok = false;
    switch (d->type) {
    case DEV_PWM:
        ok = start_pwm(d);
        break;
    case DEV_WS28XX:
        ok = start_strip(d);
        break;
    case DEV_RELAY:
        ok = start_relay(d);
        break;
    default:
        ESP_LOGE(TAG, "%s: no such device type", d->id);
        return false;
    }
    if (!ok) {
        return false;
    }
    /* Safe before anything else can command it. The window between a pin being
     * configured and being told what to do is a window where a fogger could be
     * on, and nothing is watching yet. */
    device_safe(d);
    ESP_LOGI(TAG, "%s on gpio %d", d->id, d->gpio);
    return true;
}

void device_stop(device_t *d)
{
    device_safe(d);
    if (d->type == DEV_WS28XX && d->strip) {
        led_strip_del(d->strip);
        d->strip = NULL;
    }
    /* LEDC channels and GPIO outputs are reclaimed by the next configuration
     * starting from a fresh budget; there is nothing to release that stopping
     * the output has not already done. */
    d->type = DEV_NONE;
}

void device_apply(device_t *d)
{
    switch (d->type) {
    case DEV_PWM: {
        float v = d->value[0];
        if (v < 0) v = 0;
        if (v > 1) v = 1;
        uint32_t duty = (uint32_t)(v * PWM_MAX_DUTY + 0.5f);
        ledc_set_duty(LEDC_LOW_SPEED_MODE, d->ledc, duty);
        ledc_update_duty(LEDC_LOW_SPEED_MODE, d->ledc);
        break;
    }
    case DEV_WS28XX: {
        if (!d->strip) {
            break;
        }
        uint8_t rgb[3];
        for (int i = 0; i < 3; i++) {
            float v = d->value[i];
            if (v < 0) v = 0;
            if (v > 1) v = 1;
            rgb[i] = (uint8_t)(v * 255 + 0.5f);
        }
        /* The configured channel order, or straight through when there is
         * none. A bad one is ignored and said once, because a strip showing
         * the wrong colour is already why somebody is reading this log. */
        int map[3];
        if (!device_channel_map(d->order, map) && !d->order_complained) {
            d->order_complained = true;
            ESP_LOGW(TAG, "%s: colour order \"%s\" is not a permutation of "
                          "r, g and b; sending them in order instead",
                     d->id, d->order);
        }
        int n = d->pixels > 0 ? d->pixels : 30;
        for (int i = 0; i < n; i++) {
            led_strip_set_pixel(d->strip, i,
                                rgb[map[0]], rgb[map[1]], rgb[map[2]]);
        }
        led_strip_refresh(d->strip);
        break;
    }
    case DEV_RELAY: {
        /* A relay is a dimmed output that has made up its mind. Half on is not
         * a state a contactor has, so anything above the middle is on. */
        bool on = d->value[0] >= 0.5f;
        gpio_set_level(d->gpio, on == d->active_high ? 1 : 0);
        break;
    }
    default:
        break;
    }
}

void device_safe(device_t *d)
{
    for (int i = 0; i < DEVICE_CHANNELS; i++) {
        d->value[i] = 0;
    }
    /* A strip's safe state is dark. Anything else is one number, and the
     * configuration says what it is: usually zero, and not always, because a
     * house light that fails to full is safer than one that fails to dark. */
    if (d->type != DEV_WS28XX) {
        d->value[0] = d->safe;
    }
    d->hold_until_us = 0;
    d->is_safe = true;
    device_apply(d);
}

/* One device, as the JSON a node announces for it.
 *
 * Here rather than in the node so that it can be tested without a socket. What
 * a board stores and what it says back have drifted apart twice, in both
 * directions, and each time the only way to see it was to configure real
 * hardware and read the answer.
 *
 * Everything a configuration can set appears here. Nothing else does: index is
 * the caller's, because it is a fact about the list rather than the device.
 */
cJSON *device_announcement(const device_t *d, int index)
{
    cJSON *in = cJSON_CreateObject();
    if (!in) {
        return NULL;
    }
    cJSON_AddNumberToObject(in, "index", index);
    cJSON_AddStringToObject(in, "id", d->id);
    cJSON_AddStringToObject(in, "kind", d->kind);
    cJSON_AddNumberToObject(in, "latency_ms", d->latency_ms);

    /* How it is wired. Announced because this board is the only thing that
     * knows: a studio without these has to invent a pin, and an invented pin is
     * indistinguishable from a board that lost its configuration. The same
     * argument ADR 0002 makes for latency, applied to the rest of it. */
    cJSON_AddStringToObject(in, "type", device_type_name(d->type));
    cJSON_AddNumberToObject(in, "gpio", d->gpio);
    /* The value this output falls back to, as a plain number and not only
     * inside safe_state. A fogger set to fail closed has to read back as one,
     * or the next write turns it into a fogger that fails open. */
    cJSON_AddNumberToObject(in, "safe", d->safe);
    if (d->order[0]) {
        cJSON_AddStringToObject(in, "order", d->order);
    }
    switch (d->type) {
    case DEV_PWM:
        cJSON_AddNumberToObject(in, "freq_hz", d->freq_hz);
        break;
    case DEV_WS28XX:
        cJSON_AddNumberToObject(in, "pixels", d->pixels);
        break;
    case DEV_RELAY:
        cJSON_AddStringToObject(in, "active", d->active_high ? "high" : "low");
        break;
    default:
        break;
    }
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
    return in;
}

/* Which of this device's three values feeds each of the strip's channels.
 *
 * The driver takes red, green and blue by name and lays them on the wire in
 * whatever sequence the LED model it was built for expects. That is right for
 * a plain WS2812 and wrong for the several parts that answer to the same three
 * wires with the channels shuffled, which is most of what arrives in a bag from
 * a marketplace. A strip cannot be asked what it is, so this is configuration.
 *
 * The string names which of our channels goes into the driver's red, green and
 * blue argument, in that sequence. So a strip that lights green when it is told
 * red wants \"grb\": our green into the driver's red, our red into its green.
 * That is also the name people already use for such a strip, which is the only
 * reason to prefer it over some more principled scheme nobody would guess.
 *
 * Empty, or rgb, is no permutation at all: exactly what every board did before
 * this was honoured, so nothing already working changes.
 *
 * Anything that is not a permutation of the three is ignored rather than
 * half applied. A colour order is guessed at a bench by watching a strip, and a
 * typo that silently dropped a channel would look like a broken strip.
 */
bool device_channel_map(const char *order, int map[3])
{
    map[0] = 0;
    map[1] = 1;
    map[2] = 2;
    if (!order || order[0] == 0) {
        return true;
    }

    int wanted[3];
    bool seen[3] = {false, false, false};
    for (int i = 0; i < 3; i++) {
        int c = order[i];
        int channel;
        switch (c) {
        case 'r': case 'R': channel = 0; break;
        case 'g': case 'G': channel = 1; break;
        case 'b': case 'B': channel = 2; break;
        default: return false;
        }
        if (seen[channel]) {
            return false;   /* the same channel twice leaves one unfed */
        }
        seen[channel] = true;
        wanted[i] = channel;
    }
    if (order[3] != 0) {
        return false;       /* longer than three, so it means something else */
    }
    for (int i = 0; i < 3; i++) {
        map[i] = wanted[i];
    }
    return true;
}

const char *device_type_name(device_type_t t)
{
    switch (t) {
    case DEV_PWM:    return "pwm";
    case DEV_WS28XX: return "ws28xx";
    case DEV_RELAY:  return "relay";
    default:         return "none";
    }
}

bool device_action_stops(const char *action)
{
    if (!action) {
        return false;
    }
    /* "stop" is what a conductor sends. The rest are what a person writes in a
     * score by hand, and understanding all of them costs three comparisons
     * against an output that would otherwise stay on. */
    return strcmp(action, "stop") == 0
        || strcmp(action, "off") == 0
        || strcmp(action, "safe") == 0
        || strcmp(action, "neutral") == 0;
}
