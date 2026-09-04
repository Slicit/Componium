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
#include "sacn.h"
#include "web.h"

static const char *TAG = "componium";

void componium_node_init(void);
void componium_node_serve(void);

/** Wipe and retry once. A store this device cannot read is a store it should
 *  not be carrying: the only thing in it is a network it evidently cannot
 *  reach, and the cable can put that back in thirty seconds. */
static void storage_start(void)
{
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_LOGW(TAG, "storage unreadable, clearing it");
        if (nvs_flash_erase() == ESP_OK) {
            err = nvs_flash_init();
        }
    }
    if (err != ESP_OK) {
        /* A board with no storage forgets its network and its devices, and is
         * still a board: it comes up, holds every output safe, and can be
         * provisioned over the cable. Aborting instead would reboot it for ever
         * over a flash partition that is not coming back on its own. */
        ESP_LOGE(TAG, "no storage: %s. Nothing will be remembered.",
                 esp_err_to_name(err));
    }
}

void app_main(void)
{
    storage_start();

    /* The outputs, first, at their safe values. Everything this board can drive
     * is something that moves or blows or switches mains, and the window
     * between power and provisioning is exactly when nobody is watching it. */
    componium_node_init();

    if (wifi_start() != ESP_OK) {
        /* No radio. The outputs are already safe and stay that way, and the
         * cable still works, so this is worth saying rather than rebooting
         * about. */
        ESP_LOGE(TAG, "wifi would not start; this board is offline");
    }
    improv_start();

    /* Not a timeout on the network, a timeout on waiting quietly. If the
     * network is not there yet the node still comes up: it binds, it answers
     * discovery the moment an address arrives, and the watchdog is running the
     * whole time. Blocking here for ever would mean a board that is provisioned
     * but out of range does nothing at all, including nothing safe. */
    if (!wifi_await(30000)) {
        ESP_LOGW(TAG, "no network yet, starting anyway");
    }

    /* The strip, when nothing configured is already driving it. Started
     * before the node's loop because that loop never returns, and after the
     * network because it binds a socket. It asks the node what it has, which
     * is why the node was brought up above and not here. */
    sacn_start();

    /* The board's own page, after the network and only on a board with a secret
     * to lock it with. Read only: everything that changes this device changes it
     * over CIP, where a message is signed and counted. */
    web_start();

    componium_node_serve();

    /* Everything worth doing now has a task of its own: the node's socket loop
     * and its watchdog, improv on the cable, sacn on the strip. Main holds here
     * with nothing to do, which is the point. Its stack is the smallest in the
     * system and the last thing that ran on it did not fit. */
    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
