package studio

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Slicit/componium/internal/cip"
	"github.com/Slicit/componium/internal/rig"
)

// Looking at a board, and telling it what is attached.
//
// Short lived connections on purpose. The studio holds no node open: it dials,
// asks, and hangs up. A board is driven by the conductor, and a studio that
// kept a socket to one would be a second thing heartbeating it, which is the
// shape of every bug this project has had this week.
//
// The secret is never stored here. It arrives with the request, is used for one
// exchange, and goes. Where a board is already in a rig, the rig file has it;
// where it is not, the person configuring it has it in their hand.

const nodeTimeout = 3 * time.Second

type wireDevice struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	GPIO int    `json:"gpio"`
	Kind string `json:"kind"`

	FreqHz int    `json:"freqHz,omitempty"`
	Pixels int    `json:"pixels,omitempty"`
	Active string `json:"active,omitempty"`

	LatencyMS  float64 `json:"latencyMs,omitempty"`
	RampUpMS   float64 `json:"rampUpMs,omitempty"`
	RampDownMS float64 `json:"rampDownMs,omitempty"`
	Safe       float64 `json:"safe,omitempty"`
}

func (w wireDevice) toCIP() cip.Device {
	return cip.Device{
		ID: w.ID, Type: w.Type, GPIO: w.GPIO, Kind: w.Kind,
		FreqHz: w.FreqHz, Pixels: w.Pixels, Active: w.Active,
		LatencyMS: w.LatencyMS, RampUpMS: w.RampUpMS,
		RampDownMS: w.RampDownMS, Safe: w.Safe,
	}
}

// wireNode is what the page is told about a board.
type wireNode struct {
	Name     string `json:"name,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Chip     string `json:"chip,omitempty"`
	// Instruments is what the board says is attached, which is not the same as
	// what it was configured with: it is what actually started.
	Instruments []wireNodeInstrument `json:"instruments"`
}

type wireNodeInstrument struct {
	Index     int     `json:"index"`
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	LatencyMS float64 `json:"latencyMs"`
}

func describe(c *cip.Client) wireNode {
	info := c.Info()
	out := wireNode{
		Name: info.Name, Firmware: info.Firmware, Chip: info.Chip,
		Instruments: []wireNodeInstrument{},
	}
	for _, d := range c.Devices() {
		m := d.Manifest()
		out.Instruments = append(out.Instruments, wireNodeInstrument{
			Index: d.Index(), ID: m.ID, Kind: m.Kind,
			LatencyMS: float64(m.Latency) / float64(time.Millisecond),
		})
	}
	return out
}

// normaliseNodeAddr accepts what a person plausibly types.
//
// The same lesson the rig editor learned: a device's address is very often
// first met as a URL, and a bare address with no port is the commonest thing
// anybody writes. rig.NormaliseAddr already knows the CIP port, so this is a
// call rather than a second opinion.
func normaliseNodeAddr(addr string) string {
	return rig.NormaliseAddr(addr, "cip")
}

// handleNode asks a board what it has, or tells it what it should have.
func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var want struct {
		Addr    string       `json:"addr"`
		Secret  string       `json:"secret"`
		Devices []wireDevice `json:"devices"`
		// Configure distinguishes looking from changing. Without it this only
		// reads, so a page that opens a board cannot accidentally empty it.
		Configure bool `json:"configure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}
	if want.Addr == "" {
		http.Error(w, "no address", http.StatusBadRequest)
		return
	}
	addr := want.Addr
	if normalised := normaliseNodeAddr(addr); normalised != "" {
		addr = normalised
	}

	c, err := cip.Dial(addr, nodeTimeout, want.Secret)
	if err != nil {
		// A node with a secret ignores a client without one entirely, which
		// presents as no answer rather than as a refusal. Worth saying, since
		// "no hello" and "wrong key" look identical from here.
		msg := err.Error()
		if want.Secret == "" {
			msg += "; a board that takes configuration requires its secret, " +
				"and ignores anyone who does not have it"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	defer c.Close()

	if want.Configure {
		devices := make([]cip.Device, 0, len(want.Devices))
		for _, d := range want.Devices {
			devices = append(devices, d.toCIP())
		}
		if err := c.Configure(devices); err != nil {
			// The board's own words, which name the part that was wrong.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, http.StatusOK, describe(c))
}
