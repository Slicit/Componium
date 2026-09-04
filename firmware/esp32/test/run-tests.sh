#!/bin/sh
# Run the firmware's tests on an emulated ESP32.
#
#   . $IDF_PATH/export.sh
#   firmware/esp32/test/run-tests.sh
#
# No board. QEMU runs the same binary a board would, against the same chip
# headers, which is the point: the pin rules are only worth testing if
# SOC_GPIO_VALID_OUTPUT_GPIO_MASK is the real one, and a host build would have
# to invent it.
#
# Nothing here needs the radio, which is just as well because QEMU has no wifi
# PHY and any code that reaches esp_wifi_start dies in it.
#
# Exits non zero when a test fails, when none ran, or when the board stopped
# before it finished, which on this chip is what a crash looks like from outside.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
out=${TMPDIR:-/tmp}/componium-fw-tests.log

if ! command -v idf.py > /dev/null 2>&1; then
    echo "idf.py is not on PATH. Run: . \$IDF_PATH/export.sh" >&2
    exit 2
fi

qemu=$(command -v qemu-system-xtensa 2>/dev/null || true)
if [ -z "$qemu" ]; then
    qemu=$(ls "$HOME"/.espressif/tools/qemu-xtensa/*/qemu/bin/qemu-system-xtensa 2>/dev/null | head -1 || true)
fi
if [ -z "$qemu" ]; then
    echo "no qemu-system-xtensa. Run: python3 \$IDF_PATH/tools/idf_tools.py install qemu-xtensa" >&2
    echo "(it also needs libslirp0)" >&2
    exit 2
fi

cd "$here"
idf.py build > /dev/null

cd build
esptool.py --chip=esp32 merge_bin --output=qemu_flash.bin --fill-flash-size=2MB \
    --flash_mode dio --flash_freq 40m --flash_size 2MB \
    0x1000 bootloader/bootloader.bin \
    0x8000 partition_table/partition-table.bin \
    0x10000 componium_tests.bin > /dev/null 2>&1

# An efuse block QEMU can write to. Absent, it refuses to start.
[ -f qemu_efuse.bin ] || dd if=/dev/zero of=qemu_efuse.bin bs=124 count=1 > /dev/null 2>&1

# Sixty seconds is a long way past the two the tests take. The timeout is for
# the run that never prints its last line, which is the interesting failure.
timeout 60 "$qemu" -M esp32 -m 4M \
    -drive file=qemu_flash.bin,if=mtd,format=raw \
    -drive file=qemu_efuse.bin,if=none,format=raw,id=efuse \
    -global driver=nvram.esp32.efuse,property=drive,value=efuse \
    -global driver=timer.esp32.timg,property=wdt_disable,value=true \
    -nographic > "$out" 2>&1 || true

sed -n '/Tests\|FAIL\|assert\|Guru\|Backtrace\|COMPONIUM TESTS DONE/p' "$out"

if ! grep -q "COMPONIUM TESTS DONE" "$out"; then
    echo >&2
    echo "The board stopped before the tests finished. Full log: $out" >&2
    tail -25 "$out" >&2
    exit 1
fi

# Unity prints "N Tests M Failures K Ignored".
line=$(grep -E '^[0-9]+ Tests' "$out" | tail -1)
if [ -z "$line" ]; then
    echo "no test summary in $out" >&2
    exit 1
fi

failures=$(echo "$line" | awk '{print $3}')
if [ "$failures" != "0" ]; then
    echo >&2
    echo "$line" >&2
    grep -E ':[0-9]+:.*:FAIL' "$out" >&2 || true
    exit 1
fi

echo "$line"
