/* Improv Wi-Fi over the serial line, so a password never leaves its owner. */

#pragma once

/** What this board calls itself to the flasher. */
#ifndef NODE_NAME
#define NODE_NAME "componium-node"
#endif

/** Start listening on the console UART. Returns immediately. */
void improv_start(void);
