#!/bin/sh
# Package a built firmware into something a browser can flash.
#
# esp-web-tools wants one image and a manifest describing it. The image is the
# bootloader, the partition table and the application merged into a single blob
# written at offset 0, which is the arrangement that survives a board in an
# unknown state: it does not matter what was on it before.
#
#   . $IDF_PATH/export.sh
#   cd firmware/esp32 && idf.py set-target esp32 && idf.py build
#   ./make-web-install.sh
#
# Then point the studio at the result:
#
#   componium studio -firmware firmware/esp32/web ...
#
# The output is a build artifact and is not committed. It is close to a
# megabyte, it changes on its own schedule, and a studio release has no
# business being tied to a firmware release.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
build="$here/build"
out="$here/web"

if [ ! -f "$build/componium_node.bin" ]; then
    echo "no build in $build. Run: idf.py set-target esp32 && idf.py build" >&2
    exit 1
fi
if ! command -v esptool.py > /dev/null 2>&1; then
    echo "esptool.py is not on PATH. Run: . \$IDF_PATH/export.sh" >&2
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
esptool.py --chip esp32 merge_bin \
    -o "$out/componium-node-esp32.bin" \
    --flash_mode dio --flash_size 4MB \
    0x1000 "$build/bootloader/bootloader.bin" \
    0x8000 "$build/partition_table/partition-table.bin" \
    0x10000 "$build/componium_node.bin"

cat > "$out/manifest.json" <<MANIFEST
{
  "name": "Componium node",
  "version": "$version",
  "new_install_prompt_erase": true,
  "builds": [
    {
      "chipFamily": "ESP32",
      "improv": true,
      "parts": [
        { "path": "componium-node-esp32.bin", "offset": 0 }
      ]
    }
  ]
}
MANIFEST

echo "wrote $out/manifest.json"
ls -l "$out"
