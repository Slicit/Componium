#include "led.h"

#include <string.h>

#include "esp_log.h"
#include "led_strip.h"

static const char *TAG = "led";

static led_strip_handle_t s_strip;
/* What is on the strip now, so a repeated frame costs nothing. sACN arrives at
 * the conductor's frame rate whether or not the colour changed, and pushing an
 * identical frame down the wire thirty times a second is work nobody asked
 * for. */
static uint8_t s_r, s_g, s_b;
static bool s_lit;

bool led_start(void)
{
    led_strip_config_t strip = {
        .strip_gpio_num   = LED_GPIO,
        .max_leds         = LED_COUNT,
        .led_pixel_format = LED_PIXEL_FORMAT_GRB,
        .led_model        = LED_MODEL_WS2812,
        .flags.invert_out = false,
    };
    /* RMT rather than SPI: it is on every ESP32, it needs no extra pin, and
     * one strip at this length is nowhere near its limits. */
    led_strip_rmt_config_t rmt = {
        .clk_src       = RMT_CLK_SRC_DEFAULT,
        .resolution_hz = 10 * 1000 * 1000,
        .flags.with_dma = false,
    };
    esp_err_t err = led_strip_new_rmt_device(&strip, &rmt, &s_strip);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "no strip on gpio %d: %s", LED_GPIO, esp_err_to_name(err));
        s_strip = NULL;
        return false;
    }
    /* Dark on arrival. A strip that comes up holding whatever was in its
     * registers is a strip that flashes every time the board reboots. */
    led_strip_clear(s_strip);
    s_lit = false;
    ESP_LOGI(TAG, "%d pixels on gpio %d", LED_COUNT, LED_GPIO);
    return true;
}

void led_paint(uint8_t r, uint8_t g, uint8_t b)
{
    if (!s_strip) {
        return;
    }
    if (s_lit && r == s_r && g == s_g && b == s_b) {
        return;
    }
    s_r = r; s_g = g; s_b = b; s_lit = true;
    for (int i = 0; i < LED_COUNT; i++) {
        led_strip_set_pixel(s_strip, i, r, g, b);
    }
    led_strip_refresh(s_strip);
}

void led_off(void)
{
    if (!s_strip) {
        return;
    }
    led_strip_clear(s_strip);
    s_r = s_g = s_b = 0;
    s_lit = true;
}
