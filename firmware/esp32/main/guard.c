#include "guard.h"

#include <math.h>

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
