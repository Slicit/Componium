/* What the board is told, what it stores, and what it says back.
 *
 * The three had drifted apart twice, in both directions, and each time the only
 * way to see it was to configure real hardware and read the answer. This does
 * the whole trip on the chip with no radio and no socket: the JSON a conductor
 * sends, through the parser that stores it, out through the announcement, and
 * compared field by field against what went in.
 */

#include <string.h>

#include "unity.h"
#include "cJSON.h"

#include "config.h"
#include "devices.h"

/* Every field a configuration can set, none of them left at its default, so a
 * value that fails to survive is distinguishable from one never set. */
static const char *EVERYTHING =
    "[{\"id\":\"wind.main\",\"type\":\"pwm\",\"gpio\":19,\"kind\":\"wind\","
    "\"freq_hz\":18000,\"latency_ms\":1234,\"ramp_up_ms\":1800,"
    "\"ramp_down_ms\":2900,\"safe\":0.25},"
    "{\"id\":\"light.strip\",\"type\":\"ws28xx\",\"gpio\":27,\"kind\":\"light\","
    "\"pixels\":60,\"order\":\"rgb\",\"latency_ms\":21},"
    "{\"id\":\"fog.left\",\"type\":\"relay\",\"gpio\":23,\"kind\":\"fog\","
    "\"active\":\"low\",\"latency_ms\":2100,\"safe\":1}]";

static device_t parsed_devices[DEVICE_MAX];
static char why[128];

static double num(const cJSON *o, const char *name)
{
    const cJSON *v = cJSON_GetObjectItem(o, name);
    TEST_ASSERT_TRUE_MESSAGE(cJSON_IsNumber(v), name);
    return v->valuedouble;
}

static const char *str(const cJSON *o, const char *name)
{
    const cJSON *v = cJSON_GetObjectItem(o, name);
    TEST_ASSERT_TRUE_MESSAGE(cJSON_IsString(v), name);
    return v->valuestring;
}

static void a_configuration_is_stored_field_for_field(void)
{
    int n = config_parse(EVERYTHING, parsed_devices, why, sizeof(why));
    TEST_ASSERT_EQUAL_INT_MESSAGE(3, n, why);

    const device_t *fan = &parsed_devices[0];
    TEST_ASSERT_EQUAL_STRING("wind.main", fan->id);
    TEST_ASSERT_EQUAL_STRING("wind", fan->kind);
    TEST_ASSERT_EQUAL_INT(DEV_PWM, fan->type);
    TEST_ASSERT_EQUAL_INT(19, fan->gpio);
    TEST_ASSERT_EQUAL_INT(18000, fan->freq_hz);
    TEST_ASSERT_EQUAL_FLOAT(1234.0f, fan->latency_ms);
    TEST_ASSERT_EQUAL_FLOAT(1800.0f, fan->ramp_up_ms);
    TEST_ASSERT_EQUAL_FLOAT(2900.0f, fan->ramp_down_ms);
    TEST_ASSERT_EQUAL_FLOAT(0.25f, fan->safe);

    const device_t *strip = &parsed_devices[1];
    TEST_ASSERT_EQUAL_INT(DEV_WS28XX, strip->type);
    TEST_ASSERT_EQUAL_INT(27, strip->gpio);
    TEST_ASSERT_EQUAL_INT(60, strip->pixels);
    TEST_ASSERT_EQUAL_STRING("rgb", strip->order);

    const device_t *fog = &parsed_devices[2];
    TEST_ASSERT_EQUAL_INT(DEV_RELAY, fog->type);
    TEST_ASSERT_EQUAL_INT(23, fog->gpio);
    TEST_ASSERT_FALSE(fog->active_high);       /* "low" */
    TEST_ASSERT_EQUAL_FLOAT(1.0f, fog->safe);
}

static void what_was_stored_is_what_is_announced(void)
{
    /* The half that was missing. A field the board keeps and does not announce
     * reads back as empty, and the next write sets it to empty. */
    int n = config_parse(EVERYTHING, parsed_devices, why, sizeof(why));
    TEST_ASSERT_EQUAL_INT_MESSAGE(3, n, why);
    /* channels follows from the type, and is what device_start would set. */
    for (int i = 0; i < n; i++) {
        parsed_devices[i].channels = (parsed_devices[i].type == DEV_WS28XX) ? 3 : 1;
    }

    cJSON *fan = device_announcement(&parsed_devices[0], 0);
    TEST_ASSERT_NOT_NULL(fan);
    TEST_ASSERT_EQUAL_STRING("wind.main", str(fan, "id"));
    TEST_ASSERT_EQUAL_STRING("wind", str(fan, "kind"));
    TEST_ASSERT_EQUAL_STRING("pwm", str(fan, "type"));
    TEST_ASSERT_EQUAL_INT(19, (int)num(fan, "gpio"));
    TEST_ASSERT_EQUAL_INT(18000, (int)num(fan, "freq_hz"));
    TEST_ASSERT_EQUAL_INT(1234, (int)num(fan, "latency_ms"));
    TEST_ASSERT_EQUAL_INT(1800, (int)num(fan, "ramp_up_ms"));
    TEST_ASSERT_EQUAL_INT(2900, (int)num(fan, "ramp_down_ms"));
    TEST_ASSERT_EQUAL_FLOAT(0.25f, (float)num(fan, "safe"));
    TEST_ASSERT_EQUAL_INT(0, (int)num(fan, "index"));
    cJSON_Delete(fan);

    cJSON *strip = device_announcement(&parsed_devices[1], 1);
    TEST_ASSERT_NOT_NULL(strip);
    TEST_ASSERT_EQUAL_STRING("ws28xx", str(strip, "type"));
    TEST_ASSERT_EQUAL_INT(27, (int)num(strip, "gpio"));
    TEST_ASSERT_EQUAL_INT(60, (int)num(strip, "pixels"));
    TEST_ASSERT_EQUAL_STRING("rgb", str(strip, "order"));
    TEST_ASSERT_EQUAL_INT(1, (int)num(strip, "index"));
    cJSON_Delete(strip);

    cJSON *fog = device_announcement(&parsed_devices[2], 2);
    TEST_ASSERT_NOT_NULL(fog);
    TEST_ASSERT_EQUAL_STRING("relay", str(fog, "type"));
    TEST_ASSERT_EQUAL_INT(23, (int)num(fog, "gpio"));
    TEST_ASSERT_EQUAL_STRING("low", str(fog, "active"));
    TEST_ASSERT_EQUAL_FLOAT(1.0f, (float)num(fog, "safe"));
    cJSON_Delete(fog);
}

static void an_announcement_can_be_configured_back(void)
{
    /* The property that actually matters to somebody using the page: fetch a
     * board's configuration, write it back untouched, and nothing changes.
     *
     * So the announcement is fed to the parser that accepts a configuration.
     * Anything the board says that the board cannot then be told is a field
     * that would be lost by the round trip somebody does every time they edit
     * one device out of three. */
    int n = config_parse(EVERYTHING, parsed_devices, why, sizeof(why));
    TEST_ASSERT_EQUAL_INT_MESSAGE(3, n, why);
    for (int i = 0; i < n; i++) {
        parsed_devices[i].channels = (parsed_devices[i].type == DEV_WS28XX) ? 3 : 1;
    }

    cJSON *list = cJSON_CreateArray();
    for (int i = 0; i < n; i++) {
        cJSON_AddItemToArray(list, device_announcement(&parsed_devices[i], i));
    }
    char *text = cJSON_PrintUnformatted(list);
    TEST_ASSERT_NOT_NULL(text);
    cJSON_Delete(list);

    static device_t again[DEVICE_MAX];
    int m = config_parse(text, again, why, sizeof(why));
    cJSON_free(text);
    TEST_ASSERT_EQUAL_INT_MESSAGE(3, m, why);

    for (int i = 0; i < n; i++) {
        TEST_ASSERT_EQUAL_STRING(parsed_devices[i].id, again[i].id);
        TEST_ASSERT_EQUAL_STRING(parsed_devices[i].kind, again[i].kind);
        TEST_ASSERT_EQUAL_STRING(parsed_devices[i].order, again[i].order);
        TEST_ASSERT_EQUAL_INT(parsed_devices[i].type, again[i].type);
        TEST_ASSERT_EQUAL_INT(parsed_devices[i].gpio, again[i].gpio);
        TEST_ASSERT_EQUAL_INT(parsed_devices[i].freq_hz, again[i].freq_hz);
        TEST_ASSERT_EQUAL_INT(parsed_devices[i].pixels, again[i].pixels);
        TEST_ASSERT_EQUAL_INT(parsed_devices[i].active_high, again[i].active_high);
        TEST_ASSERT_EQUAL_FLOAT(parsed_devices[i].latency_ms, again[i].latency_ms);
        TEST_ASSERT_EQUAL_FLOAT(parsed_devices[i].ramp_up_ms, again[i].ramp_up_ms);
        TEST_ASSERT_EQUAL_FLOAT(parsed_devices[i].ramp_down_ms, again[i].ramp_down_ms);
        TEST_ASSERT_EQUAL_FLOAT(parsed_devices[i].safe, again[i].safe);
    }
}

static void several_devices_are_configured_together(void)
{
    /* One board, three devices, three types, three pins. The arrangement ADR
     * 0007 exists for, checked at the layer that stores it. */
    int n = config_parse(EVERYTHING, parsed_devices, why, sizeof(why));
    TEST_ASSERT_EQUAL_INT_MESSAGE(3, n, why);
    TEST_ASSERT_EQUAL_STRING("wind.main", parsed_devices[0].id);
    TEST_ASSERT_EQUAL_STRING("light.strip", parsed_devices[1].id);
    TEST_ASSERT_EQUAL_STRING("fog.left", parsed_devices[2].id);
    /* Distinct pins and distinct types, which is what makes them separable. */
    TEST_ASSERT_NOT_EQUAL(parsed_devices[0].gpio, parsed_devices[1].gpio);
    TEST_ASSERT_NOT_EQUAL(parsed_devices[1].gpio, parsed_devices[2].gpio);
    TEST_ASSERT_NOT_EQUAL(parsed_devices[0].type, parsed_devices[1].type);
}

void register_roundtrip_tests(void)
{
    RUN_TEST(a_configuration_is_stored_field_for_field);
    RUN_TEST(what_was_stored_is_what_is_announced);
    RUN_TEST(an_announcement_can_be_configured_back);
    RUN_TEST(several_devices_are_configured_together);
}
