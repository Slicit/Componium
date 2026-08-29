// Package tune measures the machine and the player, so the conductor can
// compensate from real numbers instead of assumptions.
//
// Everything here is reported as a pair: a mean, which is a bias and can be
// subtracted, and a spread, which cannot. A fan that consistently takes 1200ms
// to spin up can be cued 1200ms early and will land correctly. A fan that
// takes somewhere between 800 and 2000ms cannot be fixed by any amount of
// measurement, and the honest thing is to report the bound.
//
// What this package cannot measure, and must never guess, is physical
// actuation latency. Nothing in software observes a fogger's heat lag without
// a sensor. That stays declared in the instrument manifest.
package tune

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Stats summarises a set of measurements.
type Stats struct {
	N    int           `json:"n"`
	Mean time.Duration `json:"mean"`
	SD   time.Duration `json:"sd"`
	Min  time.Duration `json:"min"`
	P50  time.Duration `json:"p50"`
	P95  time.Duration `json:"p95"`
	P99  time.Duration `json:"p99"`
	Max  time.Duration `json:"max"`
}

// Summarise computes statistics over a set of durations. The input is sorted
// in place.
func Summarise(d []time.Duration) Stats {
	if len(d) == 0 {
		return Stats{}
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })

	var sum float64
	for _, v := range d {
		sum += float64(v)
	}
	mean := sum / float64(len(d))
	var sq float64
	for _, v := range d {
		e := float64(v) - mean
		sq += e * e
	}

	at := func(q int) time.Duration {
		i := len(d) * q / 100
		if i >= len(d) {
			i = len(d) - 1
		}
		return d[i]
	}
	return Stats{
		N:    len(d),
		Mean: time.Duration(mean),
		SD:   time.Duration(math.Sqrt(sq / float64(len(d)))),
		Min:  d[0],
		P50:  at(50),
		P95:  at(95),
		P99:  at(99),
		Max:  d[len(d)-1],
	}
}

func (s Stats) String() string {
	return fmt.Sprintf("mean %v sd %v p95 %v max %v (n=%d)",
		round(s.Mean), round(s.SD), round(s.P95), round(s.Max), s.N)
}

func round(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(10 * time.Microsecond)
}

// Profile is what tuning produces: everything measurable about this machine
// and this player, plus the precision they can jointly achieve.
type Profile struct {
	Machine       string    `json:"machine"`
	Player        string    `json:"player"`
	PlayerVersion string    `json:"player_version,omitempty"`
	Created       time.Time `json:"created"`

	// Timer is scheduler lateness: how late a ticker actually fires. This is
	// the floor on dispatch accuracy no matter how good the clock is.
	Timer Stats `json:"timer"`
	// Query is the round trip cost of asking the player where it is.
	Query Stats `json:"query"`
	// UpdatePeriod is the smallest observed change in reported position, which
	// is the granularity the player actually exposes. For a per frame player
	// it equals the frame interval; VLC's HTTP interface shows about 247ms.
	UpdatePeriod time.Duration `json:"update_period"`
	// RateStabilityPPM is how far playback pacing strayed from realtime.
	RateStabilityPPM float64 `json:"rate_stability_ppm"`
	// PollInterval is the interval these numbers were taken at.
	PollInterval time.Duration `json:"poll_interval"`

	// Achievable is the precision this combination can reach: the polling
	// interval plus the tail of scheduler lateness. It is an estimate to
	// compare against a cue's RequiredPrecision before a film starts, not a
	// substitute for the clock's own live figure.
	Achievable time.Duration `json:"achievable_precision"`
}

// Estimate fills in Achievable from the measurements.
//
// Deliberately pessimistic: it uses the p99 of scheduler lateness rather than
// the mean, because a precision estimate that is usually right and sometimes
// optimistic is worse than one that is always conservative.
func (p *Profile) Estimate() {
	p.Achievable = p.PollInterval + p.Timer.P99
	// A player that only updates rarely cannot be anchored more finely than
	// its own update period allows once the anchor ages.
	if p.UpdatePeriod > 0 && p.UpdatePeriod > p.PollInterval {
		p.Achievable += p.Query.P95
	}
}

// DefaultPath is where a profile for this machine and player is cached.
func DefaultPath(machine, player string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = ".componium"
	}
	name := fmt.Sprintf("%s-%s.json", sanitise(machine), sanitise(player))
	return filepath.Join(dir, "componium", "tuning", name)
}

func sanitise(s string) string {
	out := []rune(s)
	for i, r := range out {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			out[i] = '-'
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

// Save writes the profile, creating parent directories as needed.
func (p *Profile) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Load reads a profile written by Save.
func Load(path string) (*Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}
