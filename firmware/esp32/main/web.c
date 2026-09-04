/* A page on port 80, for looking at the board.
 *
 * Read only, deliberately and completely. Everything that changes this device
 * changes it over CIP, where the message is signed and counted and cannot be
 * replayed. A browser can do none of that, and a configuration form here would
 * be a second way to move a relay onto a pin, reachable by anything that can
 * open a socket and guess. So: no forms, no POST, no state. It answers the
 * question "what is this board doing" and nothing else.
 *
 * Locked behind the shared secret, as HTTP Basic. Which means the secret
 * crosses the network base64 encoded, which is to say in clear text, and that
 * is worth being blunt about: it is the same secret that authorises
 * reconfiguration over CIP, so anybody who can watch this traffic can then
 * reconfigure the board. On a home LAN, to a board on your own wifi, that is
 * usually an acceptable trade for a page you can open from a phone. On a
 * network you share with people you do not know, it is not.
 *
 * A board built with no secret serves nothing at all, rather than serving to
 * everybody: an unlocked diagnostics page is a worse default than no page.
 */

#include "web.h"

#include <stdio.h>
#include <string.h>

#include "esp_http_server.h"
#include "esp_log.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "esp_app_desc.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "mbedtls/base64.h"

#include "status.h"
#include "wifi.h"

static const char *TAG = "web";

#define WEB_PORT 80

/* Long enough for the page builder and the TLS-free stack under it. Sized the
 * way everything on this board is now sized: from what it actually does, after
 * the socket loop overflowed a stack that was picked by eye. */
#define WEB_STACK 6144

/* What a wrong password costs. Not rate limiting in any serious sense, but it
 * turns a brute force over a LAN from thousands of guesses a second into two,
 * and it costs a legitimate visitor who mistyped half a second once. */
#define WRONG_PASSWORD_DELAY_MS 500

static httpd_handle_t s_server;

/* ------------------------------------------------------------------ auth */

/**
 * Whether the request carried the secret.
 *
 * Basic auth: "Authorization: Basic base64(user:password)". The user part is
 * ignored, because there is one secret and no accounts; anything before the
 * colon is accepted so that browsers which insist on a username are happy.
 */
static bool authorised(httpd_req_t *req)
{
    char header[128];
    if (httpd_req_get_hdr_value_str(req, "Authorization", header, sizeof(header)) != ESP_OK) {
        return false;
    }
    const char *prefix = "Basic ";
    if (strncmp(header, prefix, strlen(prefix)) != 0) {
        return false;
    }
    const char *encoded = header + strlen(prefix);

    unsigned char decoded[128];
    size_t n = 0;
    if (mbedtls_base64_decode(decoded, sizeof(decoded) - 1, &n,
                              (const unsigned char *)encoded, strlen(encoded)) != 0) {
        return false;
    }
    decoded[n] = 0;

    char *colon = strchr((char *)decoded, ':');
    if (!colon) {
        return false;
    }
    bool ok = node_secret_matches(colon + 1);

    /* Wiped rather than left on the stack, where the next handler's page
     * builder would be writing over it eventually and not necessarily soon. */
    memset(decoded, 0, sizeof(decoded));
    return ok;
}

static esp_err_t ask_for_the_secret(httpd_req_t *req)
{
    vTaskDelay(pdMS_TO_TICKS(WRONG_PASSWORD_DELAY_MS));
    httpd_resp_set_status(req, "401 Unauthorized");
    httpd_resp_set_hdr(req, "WWW-Authenticate", "Basic realm=\"Componium node\"");
    httpd_resp_set_type(req, "text/plain");
    httpd_resp_sendstr(req,
        "This board is locked with the same secret its conductor uses.\n"
        "Any user name; the secret is the password.\n");
    return ESP_OK;
}

/* ------------------------------------------------------------------ page */

/* Literal text, straight out, with no buffer to overflow.
 *
 * The page's head and stylesheet are about 1400 characters and used to go
 * through the formatter below, whose buffer is 512. vsnprintf truncated them
 * in the middle of the <style> block, so the tag never closed and the browser
 * read the rest of the document as CSS: a blank page, served perfectly, with
 * nothing wrong anywhere except a buffer nobody had measured against the thing
 * being put in it.
 *
 * Anything with no format arguments belongs here, which is most of the page. */
static void put(httpd_req_t *req, const char *text)
{
    httpd_resp_send_chunk(req, text, HTTPD_RESP_USE_STRLEN);
}

static void say(httpd_req_t *req, const char *fmt, ...) __attribute__((format(printf, 2, 3)));

static void say(httpd_req_t *req, const char *fmt, ...)
{
    /* Chunked, so the whole page never exists at once. A board with eight
     * devices would otherwise want a couple of kilobytes of contiguous buffer
     * at exactly the moment its heap is most fragmented. */
    char line[512];
    va_list args;
    va_start(args, fmt);
    int n = vsnprintf(line, sizeof(line), fmt, args);
    va_end(args);
    if (n < 0) {
        return;
    }
    if ((size_t)n >= sizeof(line)) {
        /* Said out loud, because the last time this happened silently it cost
         * an afternoon: the page was blank and everything else was fine. */
        ESP_LOGE(TAG, "page fragment truncated at %u of %d bytes; it will be malformed",
                 (unsigned)sizeof(line) - 1, n);
    }
    httpd_resp_send_chunk(req, line, HTTPD_RESP_USE_STRLEN);
}

static void say_uptime(httpd_req_t *req)
{
    int64_t s = esp_timer_get_time() / 1000000;
    say(req, "<dt>up</dt><dd>%lldh %lldm %llds</dd>", s / 3600, (s % 3600) / 60, s % 60);
}

static const char *bar(float v)
{
    /* Eight steps of block, because a number between nought and one is a thing
     * the eye reads faster as a length. */
    static const char *blocks[9] = {
        "", "&#9612;", "&#9612;&#9612;", "&#9612;&#9612;&#9612;",
        "&#9612;&#9612;&#9612;&#9612;", "&#9612;&#9612;&#9612;&#9612;&#9612;",
        "&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;",
        "&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;",
        "&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;&#9612;",
    };
    int i = (int)(v * 8.0f + 0.5f);
    if (i < 0) i = 0;
    if (i > 8) i = 8;
    return blocks[i];
}

static esp_err_t page(httpd_req_t *req)
{
    if (!authorised(req)) {
        return ask_for_the_secret(req);
    }

    httpd_resp_set_type(req, "text/html; charset=utf-8");
    /* A diagnostics page that has to be reloaded by hand is a diagnostics page
     * nobody watches while they drive a fan from the studio. */
    say(req, "<!doctype html><html><head><meta charset=\"utf-8\">"
             "<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">"
             "<meta http-equiv=\"refresh\" content=\"3\">"
             "<title>Componium node</title><style>"
             ":root{color-scheme:light dark}"
             "body{font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;"
             "margin:0;padding:1.5rem;background:#faf9f7;color:#1a1a1a}"
             "@media(prefers-color-scheme:dark){body{background:#15161a;color:#e8e6e3}"
             "td,th{border-color:#2c2e35!important}h1{color:#e8e6e3!important}"
             ".card{background:#1c1e23!important;border-color:#2c2e35!important}}"
             "h1{font-size:1.1rem;margin:0 0 .25rem;color:#1a1a1a}"
             ".dim{opacity:.6}"
             ".card{background:#fff;border:1px solid #e5e2dd;border-radius:6px;"
             "padding:1rem;margin:1rem 0;max-width:60rem}"
             "dl{display:grid;grid-template-columns:max-content 1fr;gap:.15rem .9rem;margin:0}"
             "dt{opacity:.6}dd{margin:0}"
             "table{border-collapse:collapse;width:100%%;font-size:13px}"
             "th,td{text-align:left;padding:.35rem .6rem;border-bottom:1px solid #e5e2dd}"
             "th{opacity:.6;font-weight:normal}"
             ".num{text-align:right;font-variant-numeric:tabular-nums}"
             ".safe{opacity:.55}.live{color:#b4532a;font-weight:bold}"
             "</style></head><body>");

    const esp_app_desc_t *app = esp_app_get_description();
    char ip[16];
    wifi_address(ip, sizeof(ip));

    put(req, "<h1>Componium node</h1>"
             "<p class=\"dim\">Read only. Everything that changes this board changes it over CIP.</p>");

    put(req, "<div class=\"card\"><dl>");
    say(req, "<dt>firmware</dt><dd>%s</dd>", app ? app->version : "unknown");
    say(req, "<dt>built</dt><dd>%s %s</dd>", app ? app->date : "?", app ? app->time : "");
    say(req, "<dt>address</dt><dd>%s</dd>", ip[0] ? ip : "not on a network");
    say(req, "<dt>wifi</dt><dd>%s</dd>", wifi_connected() ? "up" : "down");
    say_uptime(req);
    say(req, "<dt>free heap</dt><dd>%u bytes, least ever %u</dd>",
        (unsigned)esp_get_free_heap_size(), (unsigned)esp_get_minimum_free_heap_size());

    unsigned serve = 0, watchdog = 0;
    node_status_stacks(&serve, &watchdog);
    say(req, "<dt>stack spare</dt><dd>node %u bytes, watchdog %u bytes</dd>", serve, watchdog);

    uint32_t cues = 0, curves = 0, refused = 0;
    int64_t beat = 0;
    node_status_counters(&cues, &curves, &refused, &beat);
    say(req, "<dt>cues</dt><dd>%u applied, %u curve frames</dd>",
        (unsigned)cues, (unsigned)curves);
    /* Refused is here because its absence is what made a silent board so hard
     * to read: turning traffic away and hearing none look the same otherwise. */
    say(req, "<dt>refused</dt><dd>%u datagrams</dd>", (unsigned)refused);
    if (beat < 0) {
        put(req, "<dt>heartbeat</dt><dd class=\"dim\">no conductor has spoken yet</dd>");
    } else {
        say(req, "<dt>heartbeat</dt><dd>%lldms ago</dd>", beat);
    }
    put(req, "</dl></div>");

    status_device_t devices[8];
    int n = node_status_devices(devices, 8);
    put(req, "<div class=\"card\">");
    if (n == 0) {
        say(req, "<p class=\"dim\">Nothing is attached yet. A freshly flashed board says this; "
                 "the studio's Boards page is where it is told what it has.</p>");
    } else {
        put(req, "<table><tr><th>#</th><th>id</th><th>kind</th><th>type</th>"
                 "<th class=\"num\">gpio</th><th>value</th><th class=\"num\">latency</th>"
                 "<th>state</th></tr>");
        for (int i = 0; i < n; i++) {
            const status_device_t *d = &devices[i];
            say(req, "<tr><td class=\"num\">%d</td><td>%s</td><td>%s</td><td>%s</td>"
                     "<td class=\"num\">%d</td>",
                d->index, d->id, d->kind[0] ? d->kind : "&mdash;", d->type, d->gpio);
            if (d->channels == 3) {
                say(req, "<td class=\"%s\">%.2f %.2f %.2f</td>",
                    d->is_safe ? "safe" : "live",
                    d->value[0], d->value[1], d->value[2]);
            } else {
                say(req, "<td class=\"%s\">%s %.2f</td>",
                    d->is_safe ? "safe" : "live", bar(d->value[0]), d->value[0]);
            }
            say(req, "<td class=\"num\">%.0fms</td>", d->latency_ms);
            if (d->is_safe) {
                put(req, "<td class=\"safe\">safe</td>");
            } else if (d->hold_ms_left > 0) {
                say(req, "<td class=\"live\">running, %dms left</td>", d->hold_ms_left);
            } else {
                put(req, "<td class=\"live\">running</td>");
            }
            put(req, "</tr>");
        }
        put(req, "</table>");
    }
    put(req, "</div>");

    put(req, "<p class=\"dim\">Refreshes every three seconds. The secret you typed is the one "
             "that authorises configuration over CIP, and Basic auth sends it in clear text, "
             "so this page belongs on a network you trust.</p>");
    put(req, "</body></html>");
    httpd_resp_send_chunk(req, NULL, 0);
    return ESP_OK;
}

/* Anything else, so that a wrong path does not read as a board that is down. */
static esp_err_t elsewhere(httpd_req_t *req, httpd_err_code_t err)
{
    (void)err;
    if (!authorised(req)) {
        return ask_for_the_secret(req);
    }
    httpd_resp_set_status(req, "404 Not Found");
    httpd_resp_set_type(req, "text/plain");
    httpd_resp_sendstr(req, "There is one page on this board, and it is at /\n");
    return ESP_OK;
}

void web_start(void)
{
    if (!node_secret_required()) {
        /* No secret, no page. An unlocked diagnostics server on a device that
         * can start a fogger is worse than no diagnostics server. */
        ESP_LOGW(TAG, "no secret, so no web page");
        return;
    }
    if (s_server) {
        return;
    }
    httpd_config_t cfg = HTTPD_DEFAULT_CONFIG();
    cfg.server_port = WEB_PORT;
    cfg.stack_size = WEB_STACK;
    cfg.lru_purge_enable = true;
    /* Two is enough for a person with a phone and a laptop, and every socket
     * held here is one the node cannot have. */
    cfg.max_open_sockets = 3;

    esp_err_t err = httpd_start(&s_server, &cfg);
    if (err != ESP_OK) {
        /* Reported, not asserted. A board that cannot serve a page is still a
         * board that can run a show. */
        ESP_LOGE(TAG, "no web page: %s", esp_err_to_name(err));
        s_server = NULL;
        return;
    }

    httpd_uri_t root = {
        .uri = "/", .method = HTTP_GET, .handler = page, .user_ctx = NULL,
    };
    httpd_register_uri_handler(s_server, &root);
    httpd_register_err_handler(s_server, HTTPD_404_NOT_FOUND, elsewhere);

    ESP_LOGI(TAG, "web page on http://%s/ (locked)", "this board");
}
