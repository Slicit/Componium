#!/bin/sh
# Run the node firmware on an emulated network, and hand back a port.
#
#   . $IDF_PATH/export.sh
#   COMPONIUM_CIP_SECRET='...' firmware/esp32/nettest/run.sh [port]
#
# Prints the host port the board's CIP socket is reachable on, then waits. Stop
# it with ctrl-c, or give it a command to run:
#
#   run.sh 15570 -- go test ./internal/cip/ -run Emulated
#
# QEMU has no wifi PHY, so the radio is the one thing that cannot be tested this
# way. It does emulate an Ethernet controller, and ESP-IDF drives that one, so
# everything above the radio is reachable: the protocol, the parser, the
# configuration, the watchdog, the replies.
#
# Which is the half that has actually been wrong. Four faults this week were
# only ever wrong on the firmware, and every one of them lived above the radio
# and needed a datagram to arrive before it happened at all.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
port=15570
if [ $# -gt 0 ] && [ "$1" != "--" ]; then
    port=$1
    shift
fi
run_after=no
if [ "${1:-}" = "--" ]; then
    shift
    run_after=yes
fi

if ! command -v idf.py > /dev/null 2>&1; then
    echo "idf.py is not on PATH. Run: . \$IDF_PATH/export.sh" >&2
    exit 2
fi

qemu=$(command -v qemu-system-xtensa 2>/dev/null || true)
if [ -z "$qemu" ]; then
    qemu=$(ls "$HOME"/.espressif/tools/qemu-xtensa/*/qemu/bin/qemu-system-xtensa 2>/dev/null | head -1 || true)
fi
if [ -z "$qemu" ]; then
    echo "no qemu-system-xtensa. Run:" >&2
    echo "    python3 \$IDF_PATH/tools/idf_tools.py install qemu-xtensa" >&2
    echo "(it also needs libslirp0)" >&2
    exit 2
fi

if [ -z "${COMPONIUM_CIP_SECRET:-}" ]; then
    echo "warning: no COMPONIUM_CIP_SECRET, so this board will refuse every" >&2
    echo "         authenticated client and answer only unsigned traffic." >&2
fi

cd "$here"
idf.py build > /dev/null

cd build
# A fresh flash every run, which matters more here than it looks. NVS survives,
# so a configuration written by one test is read by the next, and a board that
# was left holding a WS28xx device does not boot at all: the strip driver waits
# on an RMT peripheral QEMU does not emulate, and app_main never returns from
# it. Starting clean makes a run a run.
rm -f qemu_flash.bin
esptool.py --chip=esp32 merge_bin --output=qemu_flash.bin --fill-flash-size=4MB \
    --flash_mode dio --flash_freq 40m --flash_size 4MB \
    0x1000 bootloader/bootloader.bin \
    0x8000 partition_table/partition-table.bin \
    0xf000 ota_data_initial.bin \
    0x20000 componium_nettest.bin > /dev/null 2>&1
[ -f qemu_efuse.bin ] || dd if=/dev/zero of=qemu_efuse.bin bs=124 count=1 > /dev/null 2>&1

log=${TMPDIR:-/tmp}/componium-nettest.log
"$qemu" -M esp32 -m 4M \
    -drive file=qemu_flash.bin,if=mtd,format=raw \
    -drive file=qemu_efuse.bin,if=none,format=raw,id=efuse \
    -global driver=nvram.esp32.efuse,property=drive,value=efuse \
    -global driver=timer.esp32.timg,property=wdt_disable,value=true \
    -nic "user,model=open_eth,hostfwd=udp::${port}-:5570,hostfwd=tcp::$((port + 1))-:80" \
    -nographic > "$log" 2>&1 &
board=$!
trap 'kill $board 2>/dev/null || true' EXIT INT TERM

# The board announces its address when DHCP finishes. Emulation is slow enough
# that this is seconds rather than milliseconds.
n=0
while [ $n -lt 60 ]; do
    if grep -q "node up on" "$log" 2>/dev/null; then
        break
    fi
    if ! kill -0 $board 2>/dev/null; then
        echo "the board stopped before it had an address. Log: $log" >&2
        tail -20 "$log" >&2
        exit 1
    fi
    sleep 1
    n=$((n + 1))
done
if ! grep -q "node up on" "$log" 2>/dev/null; then
    echo "no address after ${n}s. Log: $log" >&2
    tail -20 "$log" >&2
    exit 1
fi

echo "cip      127.0.0.1:${port}"
echo "web      http://127.0.0.1:$((port + 1))/"
echo "log      $log"

# Anything after -- runs against the board, and its exit code is this script's.
if [ "$run_after" = yes ]; then
    "$@"
    exit $?
fi

echo
echo "waiting; ctrl-c to stop the board"
wait $board
