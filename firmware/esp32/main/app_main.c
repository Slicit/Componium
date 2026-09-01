/* Where the board starts.
 *
 * Order matters here and the order is: be safe, be reachable, be useful. The
 * output is initialised to its safe value by the node before anything else
 * runs; provisioning comes up before the network, because a board that cannot
 * join anything still has to be able to be told what to join; and the node's
 * socket loop is last, because it never returns.
 */

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_log.h"
#include "nvs_flash.h"

#include "wifi.h"
#include "improv.h"

static const char *TAG = "componium";

void componium_node_start(void);

/** Wipe and retry once. A store this device cannot read is a store it should
 *  not be carrying: the only thing in it is a network it evidently cannot
 *  reach, and the cable can put that back in thirty seconds. */
static void storage_start(void)
{
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_LOGW(TAG, "storage unreadable, clearing it");
        ESP_ERROR_CHECK(nvs_flash_erase());
        err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(err);
}

void app_main(void)
{
    storage_start();
    ESP_ERROR_CHECK(wifi_start());
    improv_start();

    /* Not a timeout on the network, a timeout on waiting quietly. If the
     * network is not there yet the node still comes up: it binds, it answers
     * discovery the moment an address arrives, and the watchdog is running the
     * whole time. Blocking here for ever would mean a board that is provisioned
     * but out of range does nothing at all, including nothing safe. */
    if (!wifi_await(30000)) {
        ESP_LOGW(TAG, "no network yet, starting anyway");
    }

    componium_node_start();

    /* componium_node_start only returns when its socket could not be made,
     * which is not a thing to carry on from. Everything the watchdog protects
     * is already at its safe value; hold here so the log survives and so the
     * improv task keeps answering the cable. */
    ESP_LOGE(TAG, "node stopped; output is safe, waiting for a reflash");
    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
