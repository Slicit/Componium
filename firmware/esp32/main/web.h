/* The board's own page, on port 80.
 *
 * Started after the network, and only on a board that has a secret to lock it
 * with. See web.c for why it is read only and what Basic auth costs here.
 */

#ifndef COMPONIUM_WEB_H
#define COMPONIUM_WEB_H

void web_start(void);

#endif
