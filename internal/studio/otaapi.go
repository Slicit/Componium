package studio

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Slicit/componium/internal/cip"
)

// Updating a board's firmware from the studio.
//
// The studio is already the thing that holds the secrets and already serves the
// firmware, so it is the thing that can do this: it knows how to authenticate
// to the board, and it has the image the board is about to be told to fetch.
//
// The board is told a URL and the HMAC of what should be at it. Computing that
// here is the point. A person cannot be asked to hash a file and paste the
// result, and a board cannot be asked to trust an unsigned one, so the only
// place the two meet is the machine that has both the image and the secret.

// appImage is the part of a packaged firmware that an update replaces.
//
// The one at the highest offset, which is the application. An update writes the
// app slot and nothing else: the bootloader and the partition table are earlier
// in flash and cannot be replaced this way, which is why a layout change still
// needs a cable. Chosen by offset rather than by name so this does not depend on
// what the firmware build called its output, and because sending the bootloader
// by mistake would be an update that bricks a board.
func (s *Server) appImage() (path, name string, err error) {
	if s.firmware == "" {
		return "", "", fmt.Errorf("this studio was started without -firmware, " +
			"so it has no image to send")
	}
	body, err := os.ReadFile(filepath.Join(s.firmware, firmwareManifest))
	if err != nil {
		return "", "", fmt.Errorf("no firmware to send: %w", err)
	}
	var m struct {
		Builds []struct {
			Parts []struct {
				Path   string `json:"path"`
				Offset int64  `json:"offset"`
			} `json:"parts"`
		} `json:"builds"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", "", fmt.Errorf("the firmware manifest is not readable: %w", err)
	}
	if len(m.Builds) == 0 || len(m.Builds[0].Parts) == 0 {
		return "", "", fmt.Errorf("the firmware manifest describes nothing to send")
	}

	best := m.Builds[0].Parts[0]
	for _, p := range m.Builds[0].Parts {
		if p.Offset > best.Offset {
			best = p
		}
	}
	name = filepath.Base(best.Path)
	path = filepath.Join(s.firmware, name)
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("the manifest names %s and it is not there", name)
	}
	return path, name, nil
}

// reachableFrom is the address this studio has, as the board would see it.
//
// Asked of the routing table rather than configured, by opening a socket
// towards the board and reading which of this machine's addresses the kernel
// chose. Nothing is sent. A studio on a laptop with a VPN, three interfaces and
// a docker bridge has several addresses and only one of them is the one that
// board can answer, and this is the only thing that reliably knows which.
//
// The alternative is asking the browser, and the browser is frequently reaching
// the studio through an ssh tunnel at localhost, which is not somewhere a board
// on a shelf can go.
func reachableFrom(boardAddr string) (string, error) {
	c, err := net.Dial("udp", boardAddr)
	if err != nil {
		return "", fmt.Errorf("no route to %s: %w", boardAddr, err)
	}
	defer c.Close()

	host, _, err := net.SplitHostPort(c.LocalAddr().String())
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() {
		return "", fmt.Errorf("the only route to %s is over loopback, so a board "+
			"could not fetch anything from here", boardAddr)
	}
	return host, nil
}

// firmwareURL is where the board is told to go.
//
// The host the board can see, and the port this studio was told to listen on.
// Not the port the request arrived on: a request that came down an ssh tunnel
// arrived on whichever port the tunnel used at the other end, and that number
// means nothing here.
func firmwareURL(host, listen, name string) (string, error) {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "", fmt.Errorf("this studio does not know which port it is serving " +
			"on, so it cannot tell a board where to fetch from")
	}
	return "http://" + net.JoinHostPort(host, port) + "/firmware/" + name, nil
}

// handleBoardUpdate tells one board to replace its firmware.
func (s *Server) handleBoardUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var want struct {
		Board string `json:"board"`
	}
	if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	board, known := s.boards.Find(want.Board)
	listen := s.addr
	s.mu.Unlock()

	if !known {
		http.Error(w, "no board called "+want.Board, http.StatusNotFound)
		return
	}
	if board.Secret == "" {
		http.Error(w, "no secret is stored for "+board.Name+
			", and a board takes no update without one", http.StatusBadRequest)
		return
	}

	path, name, err := s.appImage()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mac, err := cip.ImageMACOf(board.Secret, path)
	if err != nil {
		http.Error(w, "could not sign the image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	host, err := reachableFrom(board.Addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	url, err := firmwareURL(host, listen, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	c, err := cip.Dial(board.Addr, checkTimeout, board.Secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer c.Close()

	if err := c.Update(url, mac); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Started, not finished. The board downloads, checks, writes and restarts on
	// its own time, and stops answering while it does. Saying so is the whole of
	// what this endpoint can honestly report.
	writeJSON(w, http.StatusOK, map[string]any{
		"started": true,
		"board":   board.Name,
		"from":    url,
		"image":   name,
		"version": s.firmwareVersion(),
		"note": "the board is downloading now and will restart. It stops " +
			"answering while that happens, and comes back on its own.",
	})
}

// firmwareVersion is what the packaged manifest calls this build.
func (s *Server) firmwareVersion() string {
	if s.firmware == "" {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(s.firmware, firmwareManifest))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	return strings.TrimSpace(m.Version)
}
