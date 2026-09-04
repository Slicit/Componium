#include "guard.h"

#include <math.h>
#include <string.h>

bool json_shallow_enough(const char *text, int len, int max_depth)
{
    if (!text || len < 0) {
        return false;
    }
    int depth = 0;
    bool in_string = false;
    bool escaped = false;

    for (int i = 0; i < len; i++) {
        char c = text[i];

        if (in_string) {
            /* An escaped anything is text, including a backslash and a quote.
             * Getting this wrong in the lenient direction would let a document
             * hide its braces from this scan and take them to the parser. */
            if (escaped) {
                escaped = false;
            } else if (c == '\\') {
                escaped = true;
            } else if (c == '"') {
                in_string = false;
            }
            continue;
        }

        if (c == '"') {
            in_string = true;
        } else if (c == '{' || c == '[') {
            if (++depth > max_depth) {
                return false;
            }
        } else if (c == '}' || c == ']') {
            depth--;
            /* Unbalanced, which the parser will refuse anyway. Said here so
             * that a document cannot drive the counter negative and then climb
             * back up past the limit without tripping it. */
            if (depth < 0) {
                return false;
            }
        }
    }
    return true;
}

float unit_value(double v)
{
    /* NaN first, because it is the one that compares false against everything
     * and so survives an ordinary pair of bounds checks untouched. */
    if (isnan(v)) {
        return 0.0f;
    }
    if (v <= 0.0) {
        return 0.0f;
    }
    if (v >= 1.0) {
        return 1.0f;
    }
    return (float)v;
}

bool constant_time_equal(const char *a, const char *b)
{
    if (!a || !b) {
        return false;
    }
    size_t na = strlen(a);
    size_t nb = strlen(b);
    /* The length folded into the difference rather than returned on, so that a
     * wrong length costs the same as a wrong byte. The loop runs over a either
     * way, so its duration depends on the secret and not on the guess. */
    unsigned char diff = (unsigned char)((na ^ nb) != 0);
    for (size_t i = 0; i < na; i++) {
        unsigned char x = (unsigned char)a[i];
        unsigned char y = (i < nb) ? (unsigned char)b[i] : 0;
        diff |= (unsigned char)(x ^ y);
    }
    return diff == 0;
}

int bounded_int(double v, int lo, int hi, int fallback)
{
    if (isnan(v)) {
        return fallback;
    }
    if (v < (double)lo) {
        return lo;
    }
    if (v > (double)hi) {
        return hi;
    }
    return (int)v;
}
