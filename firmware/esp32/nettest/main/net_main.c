/* The node, on a network, with no radio and no board.
 *
 * QEMU has no wifi PHY, which is why every test so far has stopped at the point
 * where the firmware would join a network. It does emulate an Ethernet
 * controller, and ESP-IDF has a driver for exactly that one. So the radio is
 * the only thing that cannot be tested here: everything above it can.
 *
 * That matters because of what has gone wrong. Four faults this week were only
 * ever wrong on the firmware, and every one of them lived above the radio: a
 * stack that could not hold a reply, a configuration that could be written and
 * not read, a stop the cue path never recognised, a page truncated into
 * nothing. All four needed a datagram to arrive to happen at all, and no test
 * could deliver one.
 *
 * This is the same node the board runs. Not a mock of it, not a subset: the
 * same componium_node.c, the same parser, the same watchdog, reached over UDP
 * from the host by the same Go client the conductor uses.
 *
 * What is still out of reach: joining a network, Improv over the cable, the
 * reconnect backoff, and anything that depends on RF. Those need the board.
 */

#include <stdio.h>
#include <string.h>

#include "esp_eth.h"
#include "esp_eth_netif_glue.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"

#include "web.h"

static const char *TAG = "nettest";

/* Where this node is, for anything that asks.
 *
 * web.c puts the address on the page and says whether the node is on a network.
 * On a board those answers come from the radio; here they come from the
 * emulated Ethernet, and the question is the same one. Supplying them here is
 * what lets the page itself be tested rather than only described.
 */
static char s_address[16];

void wifi_address(char *out, size_t n)
{
    strlcpy(out, s_address, n);
}

bool wifi_connected(void)
{
    return s_address[0] != 0;
}

void componium_node_init(void);
void componium_node_serve(void);

static void on_ip(void *arg, esp_event_base_t base, int32_t id, void *data)
{
    (void)arg;
    (void)base;
    (void)id;
    ip_event_got_ip_t *got = (ip_event_got_ip_t *)data;
    /* The line the test harness waits for. Printed rather than returned
     * because the harness is on the other side of a serial port. */
    snprintf(s_address, sizeof(s_address), IPSTR, IP2STR(&got->ip_info.ip));
    /* The page, once there is an address to serve it on. */
    web_start();
    ESP_LOGI(TAG, "node up on %s", s_address);
}

static void storage(void)
{
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        if (nvs_flash_erase() == ESP_OK) {
            err = nvs_flash_init();
        }
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "no storage: %s", esp_err_to_name(err));
    }
}

static void network(void)
{
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());

    esp_netif_config_t cfg = ESP_NETIF_DEFAULT_ETH();
    esp_netif_t *netif = esp_netif_new(&cfg);

    eth_mac_config_t mac_cfg = ETH_MAC_DEFAULT_CONFIG();
    eth_phy_config_t phy_cfg = ETH_PHY_DEFAULT_CONFIG();
    /* QEMU's link is up the moment it exists, and the default timeout is a
     * wait for hardware that is not there. */
    phy_cfg.autonego_timeout_ms = 100;

    esp_eth_mac_t *mac = esp_eth_mac_new_openeth(&mac_cfg);
    esp_eth_phy_t *phy = esp_eth_phy_new_dp83848(&phy_cfg);

    esp_eth_handle_t eth = NULL;
    esp_eth_config_t eth_cfg = ETH_DEFAULT_CONFIG(mac, phy);
    ESP_ERROR_CHECK(esp_eth_driver_install(&eth_cfg, &eth));

    /* The address QEMU's DHCP hands out is fixed, so the harness knows where to
     * find this without being told. */
    uint8_t addr[6] = {0x0a, 0x00, 0x02, 0x0f, 0x00, 0x01};
    esp_eth_ioctl(eth, ETH_CMD_S_MAC_ADDR, addr);

    ESP_ERROR_CHECK(esp_netif_attach(netif, esp_eth_new_netif_glue(eth)));
    ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_ETH_GOT_IP, &on_ip, NULL));
    ESP_ERROR_CHECK(esp_eth_start(eth));
}

void app_main(void)
{
    storage();

    /* Same order as the board: safe first, then reachable, then serving. */
    componium_node_init();
    network();

    ESP_LOGI(TAG, "waiting for an address");
    componium_node_serve();

    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
