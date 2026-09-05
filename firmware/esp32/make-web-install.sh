#!/bin/sh
# Package a built firmware into something a browser can flash.
#
#   . $IDF_PATH/export.sh
#   cd firmware/esp32 && idf.py set-target esp32
#   COMPONIUM_CIP_SECRET='...' idf.py build
#   ./make-web-install.sh
#
# Then point the studio at the result:
#
#   componium studio -firmware firmware/esp32/web ...
#
# Four files rather than one blob, and the difference is the whole point.
#
# It used to merge the bootloader, the partition table and the application into
# a single image written at offset 0. That survives a board in an unknown state,
# which was the reason for it, and it also covers 0x9000 to 0xf000, which is
# where nvs lives. So every flash over USB erased the wifi credentials and the
# device configuration as a side effect of how the image was packaged, and the
# board had to be provisioned and configured again every single time.
#
# Written as separate parts, nothing touches the gap where nvs is, and a board
# keeps what it knows across an update. That matters more now than it did:
# updates normally happen over the air, and a cable is what is left when
# everything else has failed. Somebody reaching for one is already having a bad
# day and should not also lose the configuration.
#
# otadata is written too, deliberately. With two app slots the bootloader reads
# it to decide which to start, and a board that had been updated over the air
# would otherwise boot the slot this flash did not write. Seeding it makes a USB
# flash mean what somebody reaching for a cable expects: run the thing I just
# installed.
#
# The output is a build artifact and is not committed. It is close to a
# megabyte, it changes on its own schedule, and a studio release has no business
# being tied to a firmware release.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
build="$here/build"
out="$here/web"

if [ ! -f "$build/componium_node.bin" ]; then
    echo "no build in $build. Run: idf.py set-target esp32 && idf.py build" >&2
    exit 1
fi

# What the build actually put in the image, rather than what you meant.
#
# The secret arrives through a CMake compile definition, and CMake reads the
# environment when it configures, not when it builds. A build directory that was
# once configured without COMPONIUM_CIP_SECRET keeps that decision: exporting
# the variable and building again recompiles nothing and quietly produces an
# image with an empty secret. Every symptom of that arrives later, on a board
# that refuses its own configuration and can only be reached with a cable.
#
# `idf.py reconfigure` is the fix. Refusing to package is how you find out you
# needed it.
if [ -n "${COMPONIUM_CIP_SECRET:-}" ]; then
    if grep -qaF "$COMPONIUM_CIP_SECRET" "$build/componium_node.bin"; then
        echo "secret: present in the image"
    else
        echo "The built image does not contain COMPONIUM_CIP_SECRET." >&2
        echo "CMake reads it when it configures, so a build directory" >&2
        echo "configured without it ignores it. Run:" >&2
        echo >&2
        echo "    COMPONIUM_CIP_SECRET='...' idf.py reconfigure && idf.py build" >&2
        echo >&2
        echo "Refusing to package a board that would refuse all configuration." >&2
        exit 1
    fi
else
    echo "secret: none, so this board will accept no configuration" >&2
fi

version=$(git -C "$here" describe --tags --always --dirty 2>/dev/null || echo unknown)

mkdir -p "$out"
cp "$build/bootloader/bootloader.bin"            "$out/bootloader.bin"
cp "$build/partition_table/partition-table.bin"  "$out/partition-table.bin"
cp "$build/ota_data_initial.bin"                 "$out/otadata.bin"
cp "$build/componium_node.bin"                   "$out/componium-node-esp32.bin"

# Offsets in decimal, because that is what the manifest format takes:
#   0x1000  = 4096     bootloader
#   0x8000  = 32768    partition table
#   0xf000  = 61440    otadata
#   0x20000 = 131072   the first app slot
#
# nvs sits at 0x9000 to 0xf000 and is not in this list, which is the point.
cat > "$out/manifest.json" <<MANIFEST
{
  "name": "Componium node",
  "version": "$version",
  "new_install_prompt_erase": false,
  "builds": [
    {
      "chipFamily": "ESP32",
      "improv": true,
      "parts": [
        { "path": "bootloader.bin", "offset": 4096 },
        { "path": "partition-table.bin", "offset": 32768 },
        { "path": "otadata.bin", "offset": 61440 },
        { "path": "componium-node-esp32.bin", "offset": 131072 }
      ]
    }
  ]
}
MANIFEST

echo "wrote $out/manifest.json"
ls -l "$out"
