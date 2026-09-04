/* The firmware's parsers, exercised on the chip they run on.
 *
 * Under QEMU rather than on a bench, so this needs no board and can be run by
 * anyone. On the target rather than on the host, because the alternative is
 * stubbing driver/ledc.h and soc/soc_caps.h and then testing the stubs: the pin
 * table below is only worth anything if SOC_GPIO_VALID_OUTPUT_GPIO_MASK is the
 * real one from the real chip headers, and a copy of it maintained by hand
 * would agree with itself for ever and with the chip until somebody changed
 * boards.
 *
 * What is tested is what arrives from outside: the JSON depth guard, the value
 * clamps, the configuration parser, and the pin rules. What is not tested here
 * is anything that needs a radio, because QEMU has no wifi PHY.
 */

#include <math.h>
#include <string.h>

#include "unity.h"

#include "config.h"
#include "devices.h"
#include "guard.h"

/* ------------------------------------------------------------ depth guard */

static void deep_document(char *out, size_t n, int depth)
{
    size_t at = 0;
    for (int i = 0; i < depth && at + 1 < n; i++) {
        out[at++] = '[';
    }
    for (int i = 0; i < depth && at + 1 < n; i++) {
        out[at++] = ']';
    }
    out[at] = 0;
}

static void ordinary_json_is_shallow_enough(void)
{
    const char *m = "{\"v\":\"0.3\",\"t\":\"cue\",\"params\":{\"intensity\":1}}";
    TEST_ASSERT_TRUE(json_shallow_enough(m, (int)strlen(m), JSON_MAX_DEPTH));
}

static void a_document_built_only_to_be_deep_is_refused(void)
{
    /* The crash this exists to prevent. cJSON's own limit is a thousand levels
     * of recursion, which is tens of kilobytes of stack on a task that has
     * eight, so the parser must never see this at all. */
    static char deep[2048];
    deep_document(deep, sizeof(deep), 500);
    TEST_ASSERT_FALSE(json_shallow_enough(deep, (int)strlen(deep), JSON_MAX_DEPTH));
}

static void the_limit_is_where_it_says_it_is(void)
{
    static char at[256], over[256];
    deep_document(at, sizeof(at), JSON_MAX_DEPTH);
    deep_document(over, sizeof(over), JSON_MAX_DEPTH + 1);
    TEST_ASSERT_TRUE(json_shallow_enough(at, (int)strlen(at), JSON_MAX_DEPTH));
    TEST_ASSERT_FALSE(json_shallow_enough(over, (int)strlen(over), JSON_MAX_DEPTH));
}

static void braces_inside_strings_are_text(void)
{
    /* A scanner that counted these would refuse perfectly ordinary documents,
     * and one that mishandled the escape would be fooled by a crafted one. */
    const char *s = "{\"id\":\"[[[[[[[[[[[[[[[[[[[[[[[[\"}";
    TEST_ASSERT_TRUE(json_shallow_enough(s, (int)strlen(s), JSON_MAX_DEPTH));
    const char *e = "{\"id\":\"a\\\"[[[[\"}";
    TEST_ASSERT_TRUE(json_shallow_enough(e, (int)strlen(e), JSON_MAX_DEPTH));
}

static void unbalanced_closers_cannot_reset_the_count(void)
{
    const char *s = "]]]]]]]]]]]]]]]]]]]]]][[[[[[[[[[[[[[[[[[[[[[";
    TEST_ASSERT_FALSE(json_shallow_enough(s, (int)strlen(s), JSON_MAX_DEPTH));
}

/* ------------------------------------------------------------- the clamps */

static void nan_becomes_dark_and_still(void)
{
    /* The whole reason unit_value exists. NaN compares false against every
     * bound, so a plain pair of range checks passes it through untouched and
     * the cast to a duty register produces whatever it produces. */
    TEST_ASSERT_EQUAL_FLOAT(0.0f, unit_value(NAN));
    TEST_ASSERT_EQUAL_FLOAT(0.0f, unit_value(-NAN));
}

static void infinities_and_wild_numbers_are_held_in_range(void)
{
    TEST_ASSERT_EQUAL_FLOAT(1.0f, unit_value(INFINITY));
    TEST_ASSERT_EQUAL_FLOAT(0.0f, unit_value(-INFINITY));
    TEST_ASSERT_EQUAL_FLOAT(1.0f, unit_value(1e300));
    TEST_ASSERT_EQUAL_FLOAT(0.0f, unit_value(-1e300));
    TEST_ASSERT_EQUAL_FLOAT(0.5f, unit_value(0.5));
    TEST_ASSERT_EQUAL_FLOAT(0.0f, unit_value(0.0));
    TEST_ASSERT_EQUAL_FLOAT(1.0f, unit_value(1.0));
}

static void bounded_int_holds_its_bounds(void)
{
    TEST_ASSERT_EQUAL_INT(30, bounded_int(NAN, 1, 300, 30));
    TEST_ASSERT_EQUAL_INT(300, bounded_int(1e12, 1, 300, 30));
    TEST_ASSERT_EQUAL_INT(1, bounded_int(-5, 1, 300, 30));
    TEST_ASSERT_EQUAL_INT(60, bounded_int(60, 1, 300, 30));
}

/* ------------------------------------------------------- the configuration */

static device_t parsed[DEVICE_MAX];
static char problem[128];

static int parse(const char *json)
{
    problem[0] = 0;
    return config_parse(json, parsed, problem, sizeof(problem));
}

static void a_good_configuration_is_taken(void)
{
    int n = parse("[{\"id\":\"wind.main\",\"type\":\"pwm\",\"gpio\":18,\"kind\":\"wind\","
                  "\"latency_ms\":1200,\"ramp_up_ms\":1800}]");
    TEST_ASSERT_EQUAL_INT(1, n);
    TEST_ASSERT_EQUAL_STRING("wind.main", parsed[0].id);
    TEST_ASSERT_EQUAL_INT(18, parsed[0].gpio);
    TEST_ASSERT_EQUAL_INT(DEV_PWM, parsed[0].type);
    TEST_ASSERT_EQUAL_FLOAT(1200.0f, parsed[0].latency_ms);
}

static void a_pixel_count_no_chip_could_hold_is_brought_back_in_range(void)
{
    /* Straight into led_strip_config_t.max_leds, which is an allocation. */
    int n = parse("[{\"id\":\"a\",\"type\":\"ws28xx\",\"gpio\":5,\"pixels\":500000}]");
    TEST_ASSERT_EQUAL_INT(1, n);
    TEST_ASSERT_TRUE(parsed[0].pixels > 0 && parsed[0].pixels <= 300);
}

static void a_safe_value_out_of_range_cannot_mean_fail_on(void)
{
    /* safe is what an output falls back to when the conductor is gone. A relay
     * reads anything at or above a half as closed, so an unclamped 99 here is a
     * fogger whose failure state is running. */
    int n = parse("[{\"id\":\"fog\",\"type\":\"relay\",\"gpio\":21,\"safe\":99}]");
    TEST_ASSERT_EQUAL_INT(1, n);
    TEST_ASSERT_TRUE(parsed[0].safe >= 0.0f && parsed[0].safe <= 1.0f);
}

static void a_frequency_of_zero_becomes_one_a_timer_can_take(void)
{
    int n = parse("[{\"id\":\"a\",\"type\":\"pwm\",\"gpio\":18,\"freq_hz\":0}]");
    TEST_ASSERT_EQUAL_INT(1, n);
    TEST_ASSERT_TRUE(parsed[0].freq_hz >= 100);
}

static void rubbish_is_refused_rather_than_parsed(void)
{
    TEST_ASSERT_EQUAL_INT(-1, parse("not json at all"));
    TEST_ASSERT_TRUE(problem[0] != 0);
    TEST_ASSERT_EQUAL_INT(-1, parse("{\"devices\":\"not an array\"}"));
    TEST_ASSERT_EQUAL_INT(-1, parse(""));
}

static void a_deep_configuration_never_reaches_the_parser(void)
{
    static char deep[2048];
    deep_document(deep, sizeof(deep), 400);
    TEST_ASSERT_EQUAL_INT(-1, parse(deep));
}

static void a_device_with_no_name_is_refused(void)
{
    TEST_ASSERT_EQUAL_INT(-1, parse("[{\"type\":\"pwm\",\"gpio\":18}]"));
}

static void two_devices_on_one_pin_are_refused(void)
{
    TEST_ASSERT_EQUAL_INT(-1, parse("[{\"id\":\"a\",\"type\":\"pwm\",\"gpio\":18},"
                                    "{\"id\":\"b\",\"type\":\"pwm\",\"gpio\":18}]"));
    TEST_ASSERT_TRUE(strstr(problem, "gpio") != NULL);
}

static void two_devices_with_one_name_are_refused(void)
{
    TEST_ASSERT_EQUAL_INT(-1, parse("[{\"id\":\"a\",\"type\":\"pwm\",\"gpio\":18},"
                                    "{\"id\":\"a\",\"type\":\"pwm\",\"gpio\":19}]"));
}

static void more_devices_than_the_board_holds_are_refused(void)
{
    /* Nine, one past DEVICE_MAX, each on its own legal pin. The parser writes
     * into a caller's array of exactly DEVICE_MAX, so this is the check that
     * keeps a configuration off the end of it. */
    char many[1024];
    int at = 0;
    static const int pins[9] = {4, 5, 13, 14, 16, 17, 18, 19, 21};
    at += snprintf(many + at, sizeof(many) - at, "[");
    for (int i = 0; i < 9; i++) {
        at += snprintf(many + at, sizeof(many) - at,
                       "%s{\"id\":\"d%d\",\"type\":\"pwm\",\"gpio\":%d}",
                       i ? "," : "", i, pins[i]);
    }
    snprintf(many + at, sizeof(many) - at, "]");
    TEST_ASSERT_EQUAL_INT(-1, parse(many));
}

static void an_unknown_device_type_is_refused(void)
{
    TEST_ASSERT_EQUAL_INT(-1, parse("[{\"id\":\"a\",\"type\":\"steam\",\"gpio\":18}]"));
    TEST_ASSERT_TRUE(strstr(problem, "device type") != NULL);
}

/* --------------------------------------------------------------- the pins */

static void the_pins_that_would_stop_the_board_are_refused(void)
{
    /* Not a list maintained here: these come from the chip's own headers, and
     * the point of running on target is that they are the real ones. */
    for (int gpio = 6; gpio <= 11; gpio++) {
        TEST_ASSERT_NOT_NULL(device_pin_problem(gpio));   /* SPI flash */
    }
    TEST_ASSERT_NOT_NULL(device_pin_problem(1));          /* console UART */
    TEST_ASSERT_NOT_NULL(device_pin_problem(3));
    TEST_ASSERT_NOT_NULL(device_pin_problem(0));          /* strapping */
    TEST_ASSERT_NOT_NULL(device_pin_problem(12));
    TEST_ASSERT_NOT_NULL(device_pin_problem(34));         /* input only */
    TEST_ASSERT_NOT_NULL(device_pin_problem(39));
    TEST_ASSERT_NOT_NULL(device_pin_problem(-1));
    TEST_ASSERT_NOT_NULL(device_pin_problem(40));
}

static void the_pins_the_bench_actually_uses_are_allowed(void)
{
    TEST_ASSERT_NULL(device_pin_problem(5));    /* the strip */
    TEST_ASSERT_NULL(device_pin_problem(18));   /* the fan */
    TEST_ASSERT_NULL(device_pin_problem(21));
}

static void device_types_are_the_three_this_build_has(void)
{
    TEST_ASSERT_EQUAL_INT(DEV_PWM, device_type_of("pwm"));
    TEST_ASSERT_EQUAL_INT(DEV_WS28XX, device_type_of("ws28xx"));
    TEST_ASSERT_EQUAL_INT(DEV_RELAY, device_type_of("relay"));
    TEST_ASSERT_EQUAL_INT(DEV_NONE, device_type_of("steam"));
    TEST_ASSERT_EQUAL_INT(DEV_NONE, device_type_of(""));
    TEST_ASSERT_EQUAL_INT(DEV_NONE, device_type_of(NULL));
}

void app_main(void)
{
    UNITY_BEGIN();

    RUN_TEST(ordinary_json_is_shallow_enough);
    RUN_TEST(a_document_built_only_to_be_deep_is_refused);
    RUN_TEST(the_limit_is_where_it_says_it_is);
    RUN_TEST(braces_inside_strings_are_text);
    RUN_TEST(unbalanced_closers_cannot_reset_the_count);

    RUN_TEST(nan_becomes_dark_and_still);
    RUN_TEST(infinities_and_wild_numbers_are_held_in_range);
    RUN_TEST(bounded_int_holds_its_bounds);

    RUN_TEST(a_good_configuration_is_taken);
    RUN_TEST(a_pixel_count_no_chip_could_hold_is_brought_back_in_range);
    RUN_TEST(a_safe_value_out_of_range_cannot_mean_fail_on);
    RUN_TEST(a_frequency_of_zero_becomes_one_a_timer_can_take);
    RUN_TEST(rubbish_is_refused_rather_than_parsed);
    RUN_TEST(a_deep_configuration_never_reaches_the_parser);
    RUN_TEST(a_device_with_no_name_is_refused);
    RUN_TEST(two_devices_on_one_pin_are_refused);
    RUN_TEST(two_devices_with_one_name_are_refused);
    RUN_TEST(more_devices_than_the_board_holds_are_refused);
    RUN_TEST(an_unknown_device_type_is_refused);

    RUN_TEST(the_pins_that_would_stop_the_board_are_refused);
    RUN_TEST(the_pins_the_bench_actually_uses_are_allowed);
    RUN_TEST(device_types_are_the_three_this_build_has);

    UNITY_END();

    /* A line the runner greps for, because Unity's own summary is printed
     * whether or not anything failed and the exit code of a board is a thing
     * that does not exist. */
    printf("COMPONIUM TESTS DONE\n");
}
