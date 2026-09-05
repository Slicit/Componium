// Package emulated drives the real firmware, running on an emulated ESP32.
//
// Not a mock and not the software node: componium_node.c, config.c, devices.c
// and guard.c, compiled from the files a board runs, booted under QEMU with an
// emulated Ethernet controller in place of the radio, and reached over UDP by
// the same client the conductor uses.
//
// It exists because of what has gone wrong. Every firmware fault found this
// week lived above the radio and needed a datagram to arrive before it happened
// at all: a stack too small to hold a reply, a configuration that could be
// written and not read, a stop the cue path never recognised. Go talking to Go
// agreed with itself throughout. These are the tests that would have disagreed.
//
// Skipped unless the image has been built, because building it needs a
// different toolchain on a different release schedule:
//
//	. $IDF_PATH/export.sh
//	cd firmware/esp32/nettest
//	COMPONIUM_CIP_SECRET='a secret' idf.py build
//	COMPONIUM_CIP_SECRET='a secret' go test ./internal/emulated/
package emulated

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
	"github.com/Slicit/componium/internal/instrument"
)

/* Emulation is slow. A board answers a hello in milliseconds on a desk and in
 * a good fraction of a second here, and none of these numbers are about the
 * protocol: they are about an Xtensa core being interpreted. */
const (
	dialTimeout  = 10 * time.Second
	bootTimeout  = 90 * time.Second
	settleWindow = 3 * time.Second
)

var board *emulated

// emulated is one running board and the ports it can be reached on.
type emulated struct {
	cmd  *exec.Cmd
	cip  string // host:port for the node
	web  string // host:port for the page
	log  *strings.Builder
	logM sync.Mutex
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	// One board for the whole package. Booting takes seconds under emulation,
	// and a board per test would spend more time starting than testing.
	b, why := start()
	if why != "" {
		// Not a failure. The toolchain is a different one on a different
		// release schedule and most people running go test will not have it.
		fmt.Fprintln(os.Stderr, "emulated: skipping:", why)
		return 0, nil
	}
	board = b
	defer b.stop()
	return m.Run(), nil
}

func secret() string { return os.Getenv("COMPONIUM_CIP_SECRET") }

// start boots a board, or says why it did not.
func start() (*emulated, string) {
	if secret() == "" {
		return nil, "COMPONIUM_CIP_SECRET is not set; it has to be the one the image was built with"
	}

	root, err := filepath.Abs(filepath.Join("..", "..", "firmware", "esp32", "nettest", "build"))
	if err != nil {
		return nil, err.Error()
	}
	image := filepath.Join(root, "componium_nettest.bin")
	if _, err := os.Stat(image); err != nil {
		return nil, "no firmware image; build firmware/esp32/nettest first"
	}
	if stale, why := olderThanSource(image); stale {
		return nil, why
	}

	qemu, why := findQemu()
	if qemu == "" {
		return nil, why
	}
	flash, err := mergeFlash(root)
	if err != nil {
		return nil, "could not build a flash image: " + err.Error()
	}

	cipPort, webPort := freePort(), freePort()
	cmd := exec.Command(qemu,
		"-M", "esp32", "-m", "4M",
		"-drive", "file="+flash+",if=mtd,format=raw",
		"-drive", "file="+filepath.Join(root, "qemu_efuse.bin")+",if=none,format=raw,id=efuse",
		"-global", "driver=nvram.esp32.efuse,property=drive,value=efuse",
		"-global", "driver=timer.esp32.timg,property=wdt_disable,value=true",
		"-nic", fmt.Sprintf("user,model=open_eth,hostfwd=udp::%d-:5570,hostfwd=tcp::%d-:80",
			cipPort, webPort),
		"-nographic",
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err.Error()
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err.Error()
	}

	b := &emulated{
		cmd: cmd,
		cip: fmt.Sprintf("127.0.0.1:%d", cipPort),
		web: fmt.Sprintf("127.0.0.1:%d", webPort),
		log: &strings.Builder{},
	}
	go b.drain(out)

	if !b.waitFor("node up on", bootTimeout) {
		b.stop()
		return nil, "the board did not reach a network in " + bootTimeout.String() +
			"; its log follows:\n" + b.logs()
	}
	return b, ""
}

// olderThanSource reports whether an image predates the firmware it was built
// from, because testing yesterday's firmware by accident is worse than not
// testing at all.
func olderThanSource(image string) (bool, string) {
	info, err := os.Stat(image)
	if err != nil {
		return true, err.Error()
	}
	dir := filepath.Join("..", "..", "firmware", "esp32", "main")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, ""
	}
	for _, e := range entries {
		s, err := e.Info()
		if err != nil {
			continue
		}
		if s.ModTime().After(info.ModTime()) {
			return true, "the firmware image is older than " + e.Name() +
				"; rebuild firmware/esp32/nettest"
		}
	}
	return false, ""
}

func findQemu() (string, string) {
	if p, err := exec.LookPath("qemu-system-xtensa"); err == nil {
		return p, ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "no qemu-system-xtensa on PATH"
	}
	found, _ := filepath.Glob(filepath.Join(home,
		".espressif", "tools", "qemu-xtensa", "*", "qemu", "bin", "qemu-system-xtensa"))
	if len(found) == 0 {
		return "", "no qemu-system-xtensa; install it with idf_tools.py install qemu-xtensa"
	}
	return found[0], ""
}

// mergeFlash writes a fresh flash image.
//
// Fresh every run, and that matters more than it looks. NVS survives, so a
// configuration written by one run is read by the next, and a board left
// holding a WS28xx device does not boot at all: the strip driver waits on an
// RMT peripheral QEMU does not emulate. Starting clean makes a run a run.
func mergeFlash(dir string) (string, error) {
	flash := filepath.Join(dir, "qemu_flash.bin")
	_ = os.Remove(flash)

	cmd := exec.Command("esptool.py", "--chip=esp32", "merge_bin",
		"--output="+flash, "--fill-flash-size=4MB",
		"--flash_mode", "dio", "--flash_freq", "40m", "--flash_size", "4MB",
		"0x1000", filepath.Join(dir, "bootloader", "bootloader.bin"),
		"0x8000", filepath.Join(dir, "partition_table", "partition-table.bin"),
		"0xf000", filepath.Join(dir, "ota_data_initial.bin"),
		// The first app slot. Two slots moved it from 0x10000, because otadata
		// and phy_init sit between the table and the application now.
		"0x20000", filepath.Join(dir, "componium_nettest.bin"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}

	efuse := filepath.Join(dir, "qemu_efuse.bin")
	if _, err := os.Stat(efuse); err != nil {
		if err := os.WriteFile(efuse, make([]byte, 124), 0o644); err != nil {
			return "", err
		}
	}
	return flash, nil
}

// freePort asks the kernel for one nobody is using.
func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 15570
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func (b *emulated) drain(r io.Reader) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		b.logM.Lock()
		b.log.WriteString(s.Text())
		b.log.WriteByte('\n')
		b.logM.Unlock()
	}
}

func (b *emulated) logs() string {
	b.logM.Lock()
	defer b.logM.Unlock()
	return b.log.String()
}

// waitFor blocks until the board says something, or gives up.
func (b *emulated) waitFor(what string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(b.logs(), what) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// seen counts how many times the board has said something.
func (b *emulated) seen(what string) int {
	return strings.Count(b.logs(), what)
}

// waitAgain blocks until the board says something it has already said.
//
// Which is what waiting for a restart means. Asking whether the log contains
// a boot line is answered by the boot that already happened, so the wait
// returns at once and the test reads a board that has not restarted yet.
func (b *emulated) waitAgain(what string, than int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if b.seen(what) > than {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (b *emulated) stop() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
	}
}

/* ------------------------------------------------------------- helpers */

func dial(t *testing.T) *cip.Client {
	t.Helper()
	c, err := cip.Dial(board.cip, dialTimeout, secret())
	if err != nil {
		t.Fatalf("could not reach the board: %v\n%s", err, board.logs())
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// page fetches the board's own status page, which is the only way to see what
// its outputs are actually holding.
func page(t *testing.T, user, pass string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+board.web+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pass != "" || user != "" {
		req.Header.Set("Authorization", "Basic "+
			base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	}
	res, err := (&http.Client{Timeout: dialTimeout}).Do(req)
	if err != nil {
		t.Fatalf("could not reach the board's page: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

// fan and fogger, because a strip cannot be started under emulation: its driver
// waits on an RMT peripheral QEMU does not have.
func twoDevices() []cip.Device {
	return []cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 19, Kind: "wind",
			FreqHz: 18000, LatencyMS: 1234, RampUpMS: 1800, RampDownMS: 2900, Safe: 0.25},
		{ID: "fog.left", Type: cip.DeviceRelay, GPIO: 23, Kind: "fog",
			Active: "low", LatencyMS: 2100},
	}
}

func configured(t *testing.T) *cip.Client {
	t.Helper()
	c := dial(t)
	if err := c.Configure(twoDevices()); err != nil {
		t.Fatalf("could not configure the board: %v\n%s", err, board.logs())
	}
	return c
}

// beat keeps the board company, the way the thing driving it would.
//
// The watchdog arms itself the first time a board hears a heartbeat and from
// then on puts every output safe 300ms after the last one. So a test that
// wants an output to stay running has to keep sending them, and a test about
// the watchdog itself stops.
func beat(t *testing.T, c *cip.Client) func() {
	t.Helper()
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				_ = c.Heartbeat()
			}
		}
	}()
	t.Cleanup(stop)
	return stop
}

func cue(t *testing.T, c *cip.Client, id, action string, params map[string]float64, hold time.Duration) {
	t.Helper()
	d, ok := c.Device(id)
	if !ok {
		t.Fatalf("the board has no %s; it has %v", id, c.Names())
	}
	/* Retried here, above the client's own retries, and that is about the
	 * emulator rather than the protocol.
	 *
	 * A cue is given up on after three attempts at 40ms, which is a
	 * deliberate number: 120ms is well inside the latency of any
	 * instrument slow enough to be reached over a network, and a cue that
	 * has not landed by then is better recorded as skipped than sent late.
	 * That is right for a board on a desk and occasionally too quick for
	 * an Xtensa core being interpreted, which answers in tens of
	 * milliseconds and sometimes in hundreds.
	 *
	 * Sending the same cue again is safe: it carries the values it wants
	 * rather than a change to them, so applying it twice is applying it
	 * once. */
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = d.Dispatch(instrument.Dispatch{
			Cue: instrument.Cue{
				Instrument: id, Action: action, Params: params, Hold: hold,
			},
		})
		if err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s %s: %v", id, action, err)
}
