#include <stdlib.h>
#include <string.h>

#include "wifi.h"

#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "esp_log.h"
#include "esp_wifi.h"
#include "esp_netif.h"
#include "esp_event.h"
#include "nvs.h"

static const char *TAG = "wifi";

/* Where credentials are kept. One namespace, two keys, nothing else. */
#define STORE      "componium"
#define KEY_SSID   "ssid"
#define KEY_PASS   "pass"

/* How many times to retry before calling a network unreachable. Provisioning
 * needs an answer in a few seconds because somebody is watching a browser wait
 * for it; a device already on a known network retries forever, because the
 * right response to the router rebooting is patience. */
#define TRIES_WHILE_PROVISIONING 4

#define CONNECTED_BIT  BIT0
#define FAILED_BIT     BIT1

static EventGroupHandle_t s_state;
static esp_netif_t       *s_netif;
static bool               s_have_target;
static int                s_tries;
static int                s_limit;      /* 0 means retry forever */
static char               s_address[16];

static void on_wifi(void *arg, esp_event_base_t base, int32_t id, void *data)
{
    (void)arg;
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        /* Started is not the same as wanted. The radio comes up on an
         * unprovisioned board so that it can be scanned with; connecting to
         * the empty string in a loop is not a thing to do while somebody is
         * waiting for that scan. */
        if (s_have_target) {
            esp_wifi_connect();
        }
        return;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        s_address[0] = 0;
        xEventGroupClearBits(s_state, CONNECTED_BIT);
        if (!s_have_target) {
            return;
        }
        /* The reason code, because without it every failure looks identical
         * from the outside: a wrong password, a network out of range and a
         * 5GHz-only SSID all present as a board that does not come up. This
         * board has no screen, so the log is the only place it can say which.
         *
         * 15 is a handshake that failed, which almost always means the
         * password. 201 is no such network, which on an ESP32 very often means
         * the network is 5GHz: there is no 5GHz radio in this chip and there
         * never was. */
        wifi_event_sta_disconnected_t *why = (wifi_event_sta_disconnected_t *)data;
        ESP_LOGW(TAG, "disconnected, reason %d%s", why->reason,
                 why->reason == WIFI_REASON_NO_AP_FOUND
                     ? " (no such network; this chip is 2.4GHz only)"
                 : why->reason == WIFI_REASON_HANDSHAKE_TIMEOUT
                     || why->reason == WIFI_REASON_4WAY_HANDSHAKE_TIMEOUT
                     ? " (handshake failed; usually the password)"
                     : "");
        if (s_limit && ++s_tries >= s_limit) {
            ESP_LOGW(TAG, "gave up after %d attempts", s_tries);
            xEventGroupSetBits(s_state, FAILED_BIT);
            return;
        }
        esp_wifi_connect();
        return;
    }
    if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        ip_event_got_ip_t *got = (ip_event_got_ip_t *)data;
        snprintf(s_address, sizeof(s_address), IPSTR, IP2STR(&got->ip_info.ip));
        s_tries = 0;
        ESP_LOGI(TAG, "up on %s", s_address);
        xEventGroupClearBits(s_state, FAILED_BIT);
        xEventGroupSetBits(s_state, CONNECTED_BIT);
        return;
    }
}

/** Read a stored string. Returns false when there is none. */
static bool remembered(const char *key, char *out, size_t n)
{
    nvs_handle_t h;
    if (nvs_open(STORE, NVS_READONLY, &h) != ESP_OK) {
        return false;
    }
    size_t len = n;
    esp_err_t err = nvs_get_str(h, key, out, &len);
    nvs_close(h);
    return err == ESP_OK && out[0] != 0;
}

static void remember(const char *ssid, const char *pass)
{
    nvs_handle_t h;
    if (nvs_open(STORE, NVS_READWRITE, &h) != ESP_OK) {
        ESP_LOGE(TAG, "cannot open the store");
        return;
    }
    nvs_set_str(h, KEY_SSID, ssid);
    nvs_set_str(h, KEY_PASS, pass);
    nvs_commit(h);
    nvs_close(h);
    /* The name, never the password. A log is the one place a secret gets
     * copied into a bug report by somebody being helpful. */
    ESP_LOGI(TAG, "remembered %s", ssid);
}

/** Point the station at a network and (re)start it. */
static void aim(const char *ssid, const char *pass, int limit)
{
    wifi_config_t cfg = { 0 };
    strlcpy((char *)cfg.sta.ssid, ssid, sizeof(cfg.sta.ssid));
    strlcpy((char *)cfg.sta.password, pass, sizeof(cfg.sta.password));
    /* The weakest security to accept, not the expected one. WPA2 as a floor
     * refuses a mixed WPA/WPA2 access point that would have worked perfectly
     * well, and refusing to join is a worse outcome here than joining an older
     * network somebody deliberately pointed us at.
     *
     * Open networks are legitimate: a dedicated cinema VLAN with no route to
     * the internet is a reasonable thing to put a node on. */
    cfg.sta.threshold.authmode = pass[0] ? WIFI_AUTH_WPA_PSK : WIFI_AUTH_OPEN;
    /* Capable, not required. WPA3 and some WPA2 networks want management frame
     * protection; requiring it would refuse the many that do not offer it. */
    cfg.sta.pmf_cfg.capable = true;
    cfg.sta.pmf_cfg.required = false;

    s_tries = 0;
    s_limit = limit;
    s_have_target = true;
    xEventGroupClearBits(s_state, CONNECTED_BIT | FAILED_BIT);

    esp_wifi_stop();
    /* Reported, not asserted. This runs from the improv task while somebody is
     * watching a browser wait for it, and ESP_ERROR_CHECK here would reboot the
     * board mid provisioning: the flasher would see the device vanish and say
     * the network was refused, which is the wrong sentence about the wrong
     * thing. A radio that will not start is a failure to join, and there is
     * already a path for that. */
    esp_err_t err = esp_wifi_set_config(WIFI_IF_STA, &cfg);
    if (err == ESP_OK) {
        err = esp_wifi_start();
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "radio would not start: %s", esp_err_to_name(err));
        s_have_target = false;
        xEventGroupSetBits(s_state, FAILED_BIT);
    }
}

esp_err_t wifi_start(void)
{
    s_state = xEventGroupCreate();
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    s_netif = esp_netif_create_default_wifi_sta();

    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&init));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(
        WIFI_EVENT, ESP_EVENT_ANY_ID, &on_wifi, NULL, NULL));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_STA_GOT_IP, &on_wifi, NULL, NULL));
    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));

    /* Power saving parks the radio between beacons and adds tens of
     * milliseconds of jitter to a datagram that has to land on a frame. The
     * whole project is about that number being small. */
    ESP_ERROR_CHECK(esp_wifi_set_ps(WIFI_PS_NONE));

    char ssid[33] = { 0 }, pass[65] = { 0 };
    if (!remembered(KEY_SSID, ssid, sizeof(ssid))) {
        /* Nothing stored, so nothing to connect to. The radio still starts,
         * because a scan needs it running and a scan is the first thing the
         * flasher asks for. This is the state a board is in the first time
         * anybody plugs it in. */
        ESP_LOGI(TAG, "no network stored, waiting to be provisioned");
        s_have_target = false;
        ESP_ERROR_CHECK(esp_wifi_start());
        return ESP_OK;
    }
    remembered(KEY_PASS, pass, sizeof(pass));
    aim(ssid, pass, 0);
    return ESP_OK;
}

bool wifi_connected(void)
{
    return s_state && (xEventGroupGetBits(s_state) & CONNECTED_BIT);
}

bool wifi_await(uint32_t timeout_ms)
{
    if (!s_state) {
        return false;
    }
    EventBits_t bits = xEventGroupWaitBits(
        s_state, CONNECTED_BIT | FAILED_BIT, pdFALSE, pdFALSE,
        timeout_ms ? pdMS_TO_TICKS(timeout_ms) : portMAX_DELAY);
    return (bits & CONNECTED_BIT) != 0;
}

bool wifi_try(const char *ssid, const char *pass, uint32_t timeout_ms)
{
    ESP_LOGI(TAG, "trying %s", ssid);
    aim(ssid, pass, TRIES_WHILE_PROVISIONING);
    if (!wifi_await(timeout_ms)) {
        ESP_LOGW(TAG, "could not join %s", ssid);
        return false;
    }
    remember(ssid, pass);
    /* Back to patience now that it is a known network. */
    s_limit = 0;
    return true;
}

void wifi_address(char *out, size_t n)
{
    strlcpy(out, s_address, n);
}

bool wifi_scan(wifi_seen each, void *ctx)
{
    /* Blocking, so this must not be called from the event loop. It is called
     * from the improv task, which exists partly for this. */
    wifi_scan_config_t cfg = { .show_hidden = false };
    if (esp_wifi_scan_start(&cfg, true) != ESP_OK) {
        return false;
    }
    uint16_t count = 0;
    if (esp_wifi_scan_get_ap_num(&count) != ESP_OK || count == 0) {
        return true;   /* the scan worked and there is nothing out there */
    }
    if (count > 24) {
        /* Enough to find yours in. The list is sorted by signal, so the ones
         * dropped are the ones you could not have joined anyway, and the
         * alternative is several kilobytes of stack on a device that has a
         * fan to run. */
        count = 24;
    }
    wifi_ap_record_t *found = calloc(count, sizeof(*found));
    if (!found) {
        return false;
    }
    bool ok = esp_wifi_scan_get_ap_records(&count, found) == ESP_OK;
    if (ok && each) {
        for (uint16_t i = 0; i < count; i++) {
            found[i].ssid[sizeof(found[i].ssid) - 1] = 0;
            if (found[i].ssid[0] == 0) {
                continue;   /* hidden, and not something anyone can pick */
            }
            each((const char *)found[i].ssid, found[i].rssi,
                 found[i].authmode != WIFI_AUTH_OPEN, ctx);
        }
    }
    free(found);
    return ok;
}
