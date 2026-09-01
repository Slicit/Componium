/* Improv Wi-Fi over the serial line.
 *
 * The protocol the browser flasher speaks once the image is on the chip:
 * https://improv-wifi.com. It exists here for one reason, and it is not
 * convenience. A network password is the user's, and the alternatives all
 * involve it leaving their hands: baked into a build with menuconfig, typed
 * into a captive portal served by a device with no certificate, or pasted to
 * whoever is doing the flashing. Improv carries it down the USB cable they are
 * already holding, from a browser tab on their own machine, into NVS.
 *
 * The frame is deliberately small enough to parse in a state machine:
 *
 *     "IMPROV" | version | type | length | payload... | checksum
 *
 * with the checksum a plain sum of everything before it. It shares the line
 * with the log, which is why the parser resynchronises on the magic rather
 * than assuming a frame starts where the last one ended.
 *
 * THE NUMBERS BELOW ARE NOT NEGOTIABLE AND NOT GUESSABLE
 *
 * The first version of this file had the command numbers written from memory
 * and every one of them was a slot out: 2 was read as "identify" when 2 is
 * "request current state". The flasher asked what state the board was in, got
 * silence, waited ten seconds and reported no Improv support. Nothing else was
 * wrong, and nothing else could have been diagnosed from the symptom.
 *
 * They are checked against the flasher's own SDK by a test, rather than
 * against anybody's recollection. See web/src/core/improv.test.ts.
 */

#include <string.h>

#include "improv.h"
#include "wifi.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "driver/uart.h"
#include "esp_log.h"
#include "esp_app_desc.h"

static const char *TAG = "improv";

#define MAGIC       "IMPROV"
#define MAGIC_LEN   6
#define VERSION     1
#define MAX_PAYLOAD 254

/* Packet types. */
#define TYPE_CURRENT_STATE 0x01
#define TYPE_ERROR_STATE   0x02
#define TYPE_RPC           0x03
#define TYPE_RPC_RESULT    0x04

/* States, as the flasher understands them. */
#define STATE_READY        0x02   /* ready, and authorisation is not required */
#define STATE_PROVISIONING 0x03
#define STATE_PROVISIONED  0x04

/* Errors. */
#define ERROR_NONE         0x00
#define ERROR_INVALID_RPC  0x01
#define ERROR_UNKNOWN_RPC  0x02
#define ERROR_CANNOT_JOIN  0x03

/* Commands, from the flasher to us. Read off the SDK, not remembered. */
#define CMD_WIFI_SETTINGS  0x01
#define CMD_REQUEST_STATE  0x02
#define CMD_REQUEST_INFO   0x03
#define CMD_REQUEST_SCAN   0x04

/* How long to give a network before saying it cannot be joined. Someone is
 * watching a spinner, so this is chosen against their patience rather than
 * against how long a slow router might take. */
#define JOIN_TIMEOUT_MS 15000

#define LINE UART_NUM_0

static void send(uint8_t type, const uint8_t *payload, uint8_t len)
{
    uint8_t frame[MAGIC_LEN + 3 + MAX_PAYLOAD + 1];
    size_t n = 0;
    memcpy(frame, MAGIC, MAGIC_LEN);
    n = MAGIC_LEN;
    frame[n++] = VERSION;
    frame[n++] = type;
    frame[n++] = len;
    if (len) {
        memcpy(frame + n, payload, len);
        n += len;
    }
    uint8_t sum = 0;
    for (size_t i = 0; i < n; i++) {
        sum = (uint8_t)(sum + frame[i]);
    }
    frame[n++] = sum;
    uart_write_bytes(LINE, (const char *)frame, n);
}

static void say_state(uint8_t state)
{
    send(TYPE_CURRENT_STATE, &state, 1);
}

static void say_error(uint8_t error)
{
    send(TYPE_ERROR_STATE, &error, 1);
}

/**
 * Answer an RPC with a list of strings.
 *
 * The reply carries the command it answers, so the flasher can tell which
 * question it is hearing back. Each string is length prefixed; there is no
 * terminator and no escaping, which is why nothing here can be longer than a
 * byte can count.
 *
 * A reply with no strings is meaningful: for the commands that stream several
 * answers, it is how the stream ends.
 */
static void say_result(uint8_t command, const char *const *strings, int count)
{
    uint8_t body[MAX_PAYLOAD];
    size_t n = 2;   /* command and total length, filled in below */
    for (int i = 0; i < count; i++) {
        size_t len = strlen(strings[i]);
        if (n + 1 + len > sizeof(body)) {
            break;
        }
        body[n++] = (uint8_t)len;
        memcpy(body + n, strings[i], len);
        n += len;
    }
    body[0] = command;
    body[1] = (uint8_t)(n - 2);
    send(TYPE_RPC_RESULT, body, (uint8_t)n);
}

/** The state to report when nobody is mid-provisioning. */
static uint8_t settled(void)
{
    return wifi_connected() ? STATE_PROVISIONED : STATE_READY;
}

/**
 * Where to find this node, as the flasher's "next" link.
 *
 * The address is the answer, because it is the line the rig file needs:
 *     addr = "192.168.1.x:5570"
 * There is no web server on this device to send anyone to.
 */
static void say_where(uint8_t command)
{
    char address[16] = { 0 };
    wifi_address(address, sizeof(address));
    const char *out[] = { address };
    say_result(command, out, 1);
}

static void do_wifi_settings(const uint8_t *data, uint8_t len)
{
    /* ssid length, ssid, password length, password. Bounds checked at every
     * step: this is the one place on the device where a stranger with a USB
     * cable chooses how long something is. */
    if (len < 1) {
        say_error(ERROR_INVALID_RPC);
        return;
    }
    uint8_t ssid_len = data[0];
    if (1 + ssid_len + 1 > len) {
        say_error(ERROR_INVALID_RPC);
        return;
    }
    uint8_t pass_len = data[1 + ssid_len];
    if (1 + ssid_len + 1 + pass_len > len) {
        say_error(ERROR_INVALID_RPC);
        return;
    }
    if (ssid_len > 32 || pass_len > 64) {
        say_error(ERROR_INVALID_RPC);
        return;
    }

    char ssid[33] = { 0 }, pass[65] = { 0 };
    memcpy(ssid, data + 1, ssid_len);
    memcpy(pass, data + 1 + ssid_len + 1, pass_len);

    say_state(STATE_PROVISIONING);
    if (!wifi_try(ssid, pass, JOIN_TIMEOUT_MS)) {
        say_error(ERROR_CANNOT_JOIN);
        say_state(STATE_READY);
        return;
    }
    say_state(STATE_PROVISIONED);
    say_where(CMD_WIFI_SETTINGS);
}

/**
 * Report the state, and where we are if we are anywhere.
 *
 * A provisioned device owes an RPC result as well as a state: the flasher
 * leaves the command pending for one and settles it itself for anything else.
 */
static void do_request_state(void)
{
    uint8_t state = settled();
    say_state(state);
    if (state == STATE_PROVISIONED) {
        say_where(CMD_REQUEST_STATE);
    }
}

/* firmware, version, chip, name. In that order: the flasher reads them
 * positionally and shows the first and the last. */
static void do_request_info(void)
{
    const esp_app_desc_t *app = esp_app_get_description();
    const char *out[] = { "Componium node", app->version, "ESP32", NODE_NAME };
    say_result(CMD_REQUEST_INFO, out, 4);
}

/**
 * Stream the networks in range, one result each, then an empty one to close.
 *
 * Worth having rather than leaving the flasher to offer a text field: an SSID
 * typed from memory with a character wrong is indistinguishable, from the
 * board's side, from a network that is out of range, and the board has no
 * screen with which to say which it was.
 */
static void report_network(const char *ssid, int rssi, bool secured, void *ctx)
{
    char strength[8];
    snprintf(strength, sizeof(strength), "%d", rssi);
    const char *row[] = { ssid, strength, secured ? "YES" : "NO" };
    say_result(CMD_REQUEST_SCAN, row, 3);
    if (ctx) {
        (*(int *)ctx)++;
    }
}

static void do_request_scan(void)
{
    int found = 0;
    if (!wifi_scan(report_network, &found)) {
        ESP_LOGW(TAG, "scan failed");
    }
    /* Nothing more coming. Also the honest answer to an empty scan. */
    say_result(CMD_REQUEST_SCAN, NULL, 0);
    ESP_LOGI(TAG, "scan reported %d networks", found);
}

static void dispatch(const uint8_t *body, uint8_t len)
{
    if (len < 2) {
        say_error(ERROR_INVALID_RPC);
        return;
    }
    uint8_t command = body[0];
    uint8_t data_len = body[1];
    if (2 + data_len > len) {
        say_error(ERROR_INVALID_RPC);
        return;
    }
    const uint8_t *data = body + 2;

    switch (command) {
    case CMD_WIFI_SETTINGS:
        do_wifi_settings(data, data_len);
        break;
    case CMD_REQUEST_STATE:
        do_request_state();
        break;
    case CMD_REQUEST_INFO:
        do_request_info();
        break;
    case CMD_REQUEST_SCAN:
        do_request_scan();
        break;
    default:
        /* Hostname and device name exist in the protocol and mean nothing to a
         * node whose name is in its rig file. Saying so is better than silence,
         * which is what a wrong number looks like from the other end. */
        say_error(ERROR_UNKNOWN_RPC);
        break;
    }
}

/**
 * Read the line for ever, resynchronising on the magic.
 *
 * Log output shares this UART, so anything that is not a frame is somebody
 * else's bytes and gets skipped rather than treated as a parse failure.
 */
static void reader(void *arg)
{
    (void)arg;
    uint8_t frame[MAGIC_LEN + 3 + MAX_PAYLOAD + 1];
    size_t have = 0;

    /* Announced rather than waited for. The flasher resets the board and then
     * asks every second until it gives up, so this is not load bearing, but it
     * shortens the common case by up to a second. */
    say_state(settled());

    for (;;) {
        uint8_t b;
        if (uart_read_bytes(LINE, &b, 1, portMAX_DELAY) != 1) {
            continue;
        }
        /* Still looking for the magic: keep only what could still become it. */
        if (have < MAGIC_LEN) {
            if (b == (uint8_t)MAGIC[have]) {
                frame[have++] = b;
            } else {
                have = (b == (uint8_t)MAGIC[0]) ? 1 : 0;
                if (have) {
                    frame[0] = b;
                }
            }
            continue;
        }
        frame[have++] = b;

        if (have == MAGIC_LEN + 1 && frame[MAGIC_LEN] != VERSION) {
            have = 0;
            continue;
        }
        if (have < MAGIC_LEN + 3) {
            continue;
        }
        uint8_t len = frame[MAGIC_LEN + 2];
        size_t whole = MAGIC_LEN + 3 + len + 1;
        if (whole > sizeof(frame)) {
            have = 0;
            continue;
        }
        if (have < whole) {
            continue;
        }

        uint8_t sum = 0;
        for (size_t i = 0; i < whole - 1; i++) {
            sum = (uint8_t)(sum + frame[i]);
        }
        if (sum == frame[whole - 1] && frame[MAGIC_LEN + 1] == TYPE_RPC) {
            dispatch(frame + MAGIC_LEN + 3, len);
        }
        have = 0;
    }
}

void improv_start(void)
{
    /* The console already owns this UART for output. Installing the driver
     * gives us reads without taking writes away, which is what lets a frame
     * and a log line share one cable. */
    if (!uart_is_driver_installed(LINE)) {
        ESP_ERROR_CHECK(uart_driver_install(LINE, 1024, 0, 0, NULL, 0));
    }
    /* A scan streams a result per network and the join blocks for seconds, so
     * this wants room. */
    xTaskCreate(reader, "improv", 6144, NULL, 4, NULL);
}
