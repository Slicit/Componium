/* Replacing the firmware without a cable. See ota.c for what is checked.
 *
 * The secret is the CIP one. It is the same string because it answers the same
 * question, which is whether the person on the other end is allowed to change
 * what this board does, and an update is the largest possible version of that.
 */

#ifndef COMPONIUM_OTA_H
#define COMPONIUM_OTA_H

#include <stdbool.h>
#include <stdint.h>

#ifndef OTA_SECRET
#define OTA_SECRET CIP_SECRET
#endif

/**
 * Fetch an image and boot it, in the background.
 *
 * mac is the HMAC-SHA256 of the image, 32 bytes, over the shared secret.
 * Returns NULL when the update has started, or why it will not.
 *
 * Starting is not finishing. The download outlasts any socket the instruction
 * arrived on, so the answer to whether it worked is that the board either comes
 * back running something new or comes back running this.
 */
const char *ota_start(const char *url, const uint8_t *mac);

/**
 * Say that this image works, so the bootloader stops holding the old one.
 *
 * A new image boots on probation and is put back on the next restart unless it
 * says otherwise. Called once the board has an address and somebody has
 * authenticated to it, because those are the two things a bad image would fail
 * at and the two a board can check about itself.
 */
void ota_this_image_works(void);

/** The version string of the image that is running. */
const char *ota_running_version(void);

#endif
