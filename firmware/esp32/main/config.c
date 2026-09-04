/* What is attached to this board, and where it is remembered.
 *
 * The configuration is the JSON the conductor sent, kept verbatim in NVS and
 * parsed again at boot. Verbatim on purpose: a board that re-serialises what it
 * was told is a board that can quietly drop a field it does not understand yet,
 * and the next firmware would never know it had been given one.
 *
 * Accepted whole or refused whole. Half a configuration is worse than none: it
 * is a board that looks configured and is not, and nothing downstream can tell
 * the difference from one that was set up carefully.
 */

#include "config.h"
#include "guard.h"

#include <string.h>

#include "cJSON.h"
#include "esp_log.h"
#include "nvs.h"

static const char *TAG = "config";

#define STORE "componium"
#define KEY   "devices"

/* Generous for eight devices and small enough that a stranger cannot fill the
 * flash by sending one. */
#define CONFIG_MAX 4096

void device_reset_budget(void);

static float number(const cJSON *o, const char *name, float fallback)
{
    const cJSON *v = cJSON_GetObjectItem(o, name);
    return cJSON_IsNumber(v) ? (float)v->valuedouble : fallback;
}

static void text(const cJSON *o, const char *name, char *out, size_t n)
{
    const cJSON *v = cJSON_GetObjectItem(o, name);
    out[0] = 0;
    if (cJSON_IsString(v) && v->valuestring) {
        strlcpy(out, v->valuestring, n);
    }
}

int config_parse(const char *json, device_t *out, char *problem, size_t problem_len)
{
    problem[0] = 0;
    /* This arrives from the network on one path and from flash on the other,
     * and flash holds whatever the network last sent, so both are outside. */
    if (!json_shallow_enough(json, (int)strlen(json), JSON_MAX_DEPTH)) {
        snprintf(problem, problem_len, "nested deeper than %d", JSON_MAX_DEPTH);
        return -1;
    }
    cJSON *root = cJSON_Parse(json);
    if (!root) {
        snprintf(problem, problem_len, "not JSON");
        return -1;
    }
    const cJSON *list = cJSON_IsArray(root) ? root : cJSON_GetObjectItem(root, "devices");
    if (!cJSON_IsArray(list)) {
        snprintf(problem, problem_len, "no devices array");
        cJSON_Delete(root);
        return -1;
    }

    int n = 0;
    const cJSON *item = NULL;
    cJSON_ArrayForEach(item, list) {
        if (n >= DEVICE_MAX) {
            snprintf(problem, problem_len, "more than %d devices", DEVICE_MAX);
            cJSON_Delete(root);
            return -1;
        }
        device_t *d = &out[n];
        memset(d, 0, sizeof(*d));

        text(item, "id", d->id, sizeof(d->id));
        text(item, "kind", d->kind, sizeof(d->kind));
        text(item, "order", d->order, sizeof(d->order));
        if (d->id[0] == 0) {
            snprintf(problem, problem_len, "device %d has no id", n + 1);
            cJSON_Delete(root);
            return -1;
        }

        char type[16];
        text(item, "type", type, sizeof(type));
        d->type = device_type_of(type);
        if (d->type == DEV_NONE) {
            snprintf(problem, problem_len, "%s: this build has no device type \"%s\"",
                     d->id, type);
            cJSON_Delete(root);
            return -1;
        }

        d->gpio = (int)number(item, "gpio", -1);
        const char *why = device_pin_problem(d->gpio);
        if (why) {
            snprintf(problem, problem_len, "%s: gpio %d is %s", d->id, d->gpio, why);
            cJSON_Delete(root);
            return -1;
        }

        /* Bounded, all of them. Unbounded, pixels asks the strip driver for a
         * buffer the chip has not got, and a frequency of zero is a timer that
         * will not configure. The upper bounds are what one board can actually
         * drive rather than what the field can hold. */
        d->freq_hz = bounded_int(number(item, "freq_hz", 25000), 100, 40000, 25000);
        d->pixels = bounded_int(number(item, "pixels", 30), 1, 300, 30);
        char active[8];
        text(item, "active", active, sizeof(active));
        d->active_high = strcmp(active, "low") != 0;

        d->latency_ms = number(item, "latency_ms", 0);
        d->ramp_up_ms = number(item, "ramp_up_ms", 0);
        d->ramp_down_ms = number(item, "ramp_down_ms", 0);
        /* The value an output falls back to when the conductor is gone, so
         * out of range here is a fogger whose failure state is "on". */
        d->safe = unit_value(number(item, "safe", 0));

        /* Two devices on one pin is a configuration nobody meant to write, and
         * the second one would quietly win. */
        for (int i = 0; i < n; i++) {
            if (out[i].gpio == d->gpio) {
                snprintf(problem, problem_len, "%s and %s both claim gpio %d",
                         out[i].id, d->id, d->gpio);
                cJSON_Delete(root);
                return -1;
            }
            if (strcmp(out[i].id, d->id) == 0) {
                snprintf(problem, problem_len, "two devices called \"%s\"", d->id);
                cJSON_Delete(root);
                return -1;
            }
        }
        n++;
    }
    cJSON_Delete(root);
    return n;
}

bool config_save(const char *json)
{
    if (strlen(json) >= CONFIG_MAX) {
        return false;
    }
    nvs_handle_t h;
    if (nvs_open(STORE, NVS_READWRITE, &h) != ESP_OK) {
        return false;
    }
    esp_err_t err = nvs_set_str(h, KEY, json);
    if (err == ESP_OK) {
        err = nvs_commit(h);
    }
    nvs_close(h);
    return err == ESP_OK;
}

int config_load(char *json, size_t len)
{
    nvs_handle_t h;
    if (nvs_open(STORE, NVS_READONLY, &h) != ESP_OK) {
        json[0] = 0;
        return 0;
    }
    size_t n = len;
    esp_err_t err = nvs_get_str(h, KEY, json, &n);
    nvs_close(h);
    if (err != ESP_OK) {
        /* Nothing stored. An ordinary state, and the one every freshly flashed
         * board is in: it announces no instruments and waits to be told. */
        json[0] = 0;
        return 0;
    }
    return (int)strlen(json);
}

void config_forget(void)
{
    nvs_handle_t h;
    if (nvs_open(STORE, NVS_READWRITE, &h) != ESP_OK) {
        return;
    }
    nvs_erase_key(h, KEY);
    nvs_commit(h);
    nvs_close(h);
    ESP_LOGW(TAG, "configuration erased");
}
