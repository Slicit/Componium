/* Replacing the firmware without a cable.
 *
 * A board in a ceiling should not need a ladder to be updated, and should still
 * be recoverable with one. So this is arranged around the assumption that it
 * will go wrong: nothing the board is currently running is touched until the
 * new image has arrived whole and been proved to be the one somebody meant to
 * send, and even then the old image stays where it is.
 *
 * # What is checked, and why it is not enough that the instruction was signed
 *
 * The update message is signed like every other control message, which says it
 * came from somebody holding the secret. That authenticates the instruction and
 * nothing else. The instruction names a URL, and whatever answers that URL is
 * authenticated by nothing at all: a board that trusted it would run whatever
 * the network handed back, which on a device that switches mains is the worst
 * thing in the protocol.
 *
 * So the message also carries an HMAC of the image over the same secret, and it
 * is checked against what actually arrived before the image is made bootable.
 * The image is not secret and does not need to be. It needs to be provably the
 * one that was meant, and that is what an HMAC says.
 *
 * An update with no MAC is refused. This is the one message that replaces the
 * code which checks every other message, so it does not get a lenient path.
 *
 * # Failing safely
 *
 * The image is written to the slot the board is not running from, so a download
 * that stops halfway leaves a working board. The boot partition is only moved
 * once the whole image has been verified. And the new image boots on probation:
 * the bootloader will put the old one back unless the new one says it is well,
 * which it only does once it has an address and somebody has spoken to it. An
 * image that cannot join a network, or was built with the wrong secret, undoes
 * itself on the next power cycle instead of needing the ladder.
 */

#include "ota.h"

#include <string.h>

#include "esp_app_desc.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_ota_ops.h"
#include "esp_system.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "mbedtls/md.h"

static const char *TAG = "ota";

/* Room for the HTTP client, the flash writes and an HMAC context. Sized like
 * everything else on this board now: from what it does, after a socket loop
 * once overflowed a stack somebody had picked by eye. */
#define OTA_TASK_STACK 8192

/* Read in chunks. Larger is faster and this is not a race: an update happens
 * between shows, and a board that spends ten seconds instead of six is not a
 * board anybody is waiting on. */
#define OTA_CHUNK 1024

/* Nothing this board runs is close to the slot size, and an image claiming to
 * be is either wrong or hostile. */
#define OTA_MAX_BYTES (1500 * 1024)

/* One at a time. Two updates at once would have two writers on the same
 * partition, which is the fault this project has spent a week not having. */
static volatile bool s_running;

/* Whether this image has said it is well. Once, and never again: the call is
 * cheap but it writes to flash, and a board that made it every minute would
 * wear otadata out for nothing. */
static bool s_confirmed;

struct plan {
    char    url[256];
    uint8_t want[32];
};

/* What the board is running, and whether the bootloader is still deciding. */
static bool on_probation(void)
{
    const esp_partition_t *running = esp_ota_get_running_partition();
    esp_ota_img_states_t state;
    if (!running || esp_ota_get_state_partition(running, &state) != ESP_OK) {
        return false;
    }
    return state == ESP_OTA_IMG_PENDING_VERIFY;
}

void ota_this_image_works(void)
{
    if (s_confirmed) {
        return;
    }
    s_confirmed = true;
    if (!on_probation()) {
        return;
    }
    /* The image has an address and somebody has authenticated to it, which is
     * as much as a board can know about its own health. Anything worse than
     * this and it would not be the thing making the call. */
    if (esp_ota_mark_app_valid_cancel_rollback() == ESP_OK) {
        ESP_LOGI(TAG, "this image works; the old one will not be restored");
    }
}

static bool same_mac(const uint8_t *a, const uint8_t *b)
{
    /* Constant time, because the comparison is between something a stranger
     * chose and something derived from the secret. */
    uint8_t diff = 0;
    for (int i = 0; i < 32; i++) {
        diff |= (uint8_t)(a[i] ^ b[i]);
    }
    return diff == 0;
}

static void fetch(void *arg)
{
    struct plan *plan = (struct plan *)arg;
    const esp_partition_t *target = esp_ota_get_next_update_partition(NULL);
    esp_http_client_handle_t http = NULL;
    esp_ota_handle_t writing = 0;
    bool began = false;
    uint8_t *buf = NULL;
    mbedtls_md_context_t md;
    bool md_ready = false;

    if (!target) {
        ESP_LOGE(TAG, "no spare slot to write; this build has one app partition");
        goto done;
    }
    ESP_LOGI(TAG, "updating into %s from %s", target->label, plan->url);

    buf = malloc(OTA_CHUNK);
    if (!buf) {
        ESP_LOGE(TAG, "no memory for the download");
        goto done;
    }

    mbedtls_md_init(&md);
    md_ready = true;
    const mbedtls_md_info_t *info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    if (mbedtls_md_setup(&md, info, 1) != 0 ||
        mbedtls_md_hmac_starts(&md, (const uint8_t *)OTA_SECRET, sizeof(OTA_SECRET) - 1) != 0) {
        ESP_LOGE(TAG, "could not start the hmac");
        goto done;
    }

    esp_http_client_config_t cfg = {
        .url = plan->url,
        .timeout_ms = 15000,
        .keep_alive_enable = true,
    };
    http = esp_http_client_init(&cfg);
    if (!http) {
        ESP_LOGE(TAG, "could not open %s", plan->url);
        goto done;
    }
    if (esp_http_client_open(http, 0) != ESP_OK) {
        ESP_LOGE(TAG, "could not reach %s", plan->url);
        goto done;
    }
    esp_http_client_fetch_headers(http);
    int status = esp_http_client_get_status_code(http);
    if (status != 200) {
        ESP_LOGE(TAG, "%s answered %d", plan->url, status);
        goto done;
    }

    if (esp_ota_begin(target, OTA_SIZE_UNKNOWN, &writing) != ESP_OK) {
        ESP_LOGE(TAG, "could not open %s for writing", target->label);
        goto done;
    }
    began = true;

    int total = 0;
    for (;;) {
        int n = esp_http_client_read(http, (char *)buf, OTA_CHUNK);
        if (n < 0) {
            ESP_LOGE(TAG, "the download failed after %d bytes", total);
            goto done;
        }
        if (n == 0) {
            break;
        }
        total += n;
        if (total > OTA_MAX_BYTES) {
            ESP_LOGE(TAG, "the image is larger than any firmware this board runs");
            goto done;
        }
        if (mbedtls_md_hmac_update(&md, buf, n) != 0) {
            ESP_LOGE(TAG, "hmac failed");
            goto done;
        }
        if (esp_ota_write(writing, buf, n) != ESP_OK) {
            ESP_LOGE(TAG, "could not write flash after %d bytes", total);
            goto done;
        }
    }

    if (total == 0) {
        ESP_LOGE(TAG, "the image was empty");
        goto done;
    }

    uint8_t got[32];
    if (mbedtls_md_hmac_finish(&md, got) != 0) {
        ESP_LOGE(TAG, "hmac failed");
        goto done;
    }
    if (!same_mac(got, plan->want)) {
        /* The bytes are not the ones the instruction described. Whether that is
         * a wrong file, a proxy, or somebody answering the URL, it is the same
         * answer: this is not the image and it will not be booted. */
        ESP_LOGE(TAG, "the image does not match its signature; refusing it");
        goto done;
    }

    if (esp_ota_end(writing) != ESP_OK) {
        ESP_LOGE(TAG, "the image did not close cleanly; refusing it");
        began = false;
        goto done;
    }
    began = false;

    if (esp_ota_set_boot_partition(target) != ESP_OK) {
        ESP_LOGE(TAG, "could not choose %s to boot", target->label);
        goto done;
    }

    ESP_LOGW(TAG, "%d bytes verified; restarting into %s", total, target->label);
    /* Long enough for the log line to leave the cable, which is the only thing
     * anybody watching has. */
    vTaskDelay(pdMS_TO_TICKS(500));
    esp_restart();

done:
    if (began) {
        esp_ota_abort(writing);
    }
    if (http) {
        esp_http_client_cleanup(http);
    }
    if (md_ready) {
        mbedtls_md_free(&md);
    }
    free(buf);
    free(plan);
    s_running = false;
    ESP_LOGW(TAG, "still running the image it started with");
    vTaskDelete(NULL);
}

const char *ota_start(const char *url, const uint8_t *mac)
{
    if (sizeof(OTA_SECRET) <= 1) {
        return "this node takes no updates without a secret";
    }
    if (!url || !url[0]) {
        return "no url";
    }
    if (!mac) {
        return "an update needs the image's signature";
    }
    if (s_running) {
        return "already updating";
    }

    struct plan *plan = calloc(1, sizeof(*plan));
    if (!plan) {
        return "out of memory";
    }
    if (strlen(url) >= sizeof(plan->url)) {
        free(plan);
        return "that url is too long";
    }
    strlcpy(plan->url, url, sizeof(plan->url));
    memcpy(plan->want, mac, sizeof(plan->want));

    s_running = true;
    if (xTaskCreate(fetch, "ota", OTA_TASK_STACK, plan, 4, NULL) != pdPASS) {
        s_running = false;
        free(plan);
        return "no room to start the update";
    }
    /* Accepted, not finished. The download takes longer than any sender should
     * hold a socket open for, so the answer to "did it work" is that the board
     * either comes back running something new or comes back running this. */
    return NULL;
}

const char *ota_running_version(void)
{
    const esp_app_desc_t *app = esp_app_get_description();
    return app ? app->version : "unknown";
}
