package rig

import (
	"fmt"
	"strconv"
	"strings"
)

// Writing the file by hand rather than with the encoder.
//
// Two reasons, and the first is a hard one. BurntSushi's `omitempty` does not
// treat a numeric zero as empty: it covers strings, slices, maps, pointers and
// bools and stops there. So an encoded rig gives every virtual fogger a
// `universe = 0` and a `start = 0`, and a DMX start address of 0 is not an
// address. The file would then fail the validation that wrote it.
//
// The second reason is that this file is read by people. It has always been
// hand edited and it stays hand editable, so the order of the keys is chosen
// rather than whatever the struct happens to be, and what an instrument is
// comes before where it is plugged in.

type lines struct{ b strings.Builder }

func (l *lines) raw(s string)              { l.b.WriteString(s) }
func (l *lines) str(k, v string)           { fmt.Fprintf(&l.b, "%s = %s\n", k, strconv.Quote(v)) }
func (l *lines) int(k string, v int64)     { fmt.Fprintf(&l.b, "%s = %d\n", k, v) }
func (l *lines) float(k string, v float64) { fmt.Fprintf(&l.b, "%s = %s\n", k, decimal(v)) }

// decimal always carries a point, because TOML says an integer is not a float
// and a strict decoder agrees. A position of exactly 1 written as "1" comes
// back as an error rather than as 1.0.
func decimal(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// encode renders a rig as TOML.
func encode(c *Config) string {
	var l lines
	l.raw(header)
	l.raw("[rig]\n")
	l.str("name", c.Rig.Name)

	for _, in := range c.Instruments {
		l.raw("\n[[instrument]]\n")
		// What it is.
		l.str("id", in.ID)
		l.str("kind", in.Kind)
		if in.Driver != "" {
			l.str("driver", in.Driver)
		}
		// Where it is plugged in.
		if in.Addr != "" {
			l.str("addr", in.Addr)
		}
		if in.Universe != 0 {
			l.int("universe", int64(in.Universe))
		}
		if in.Start != 0 {
			l.int("start", int64(in.Start))
		}
		if in.Mode != "" {
			l.str("mode", in.Mode)
		}
		if in.Format != "" {
			l.str("format", in.Format)
		}
		// How it behaves.
		if in.Latency != 0 {
			l.str("latency", in.Latency.Duration().String())
		}
		if in.RemoteTimeout != 0 {
			l.str("remote_timeout", in.RemoteTimeout.Duration().String())
		}
		if in.Secret != "" {
			l.str("secret", in.Secret)
		}

		// How it is corrected. Written only when set, so a rig nobody has
		// trimmed reads exactly as it did before these existed.
		if in.Brightness != 0 {
			l.float("brightness", in.Brightness)
		}
		if in.Saturation != 0 {
			l.float("saturation", in.Saturation)
		}

		// Tables last, because everything after one belongs to it.
		if p := in.Position; p != nil {
			l.raw("\n[instrument.position]\n")
			l.float("x", p.X)
			l.float("y", p.Y)
			l.float("z", p.Z)
		}
		if t := in.Travel; t != nil {
			l.raw("\n[instrument.travel]\n")
			l.float("surge", t.Surge)
			l.float("sway", t.Sway)
			l.float("heave", t.Heave)
			l.float("roll", t.Roll)
			l.float("pitch", t.Pitch)
			l.float("yaw", t.Yaw)
		}
		if len(in.Scents) > 0 {
			l.raw("\n[instrument.scents]\n")
			// Numbered, so in number order rather than string order: 10 comes
			// after 9 in a reservoir rack and should here too.
			for _, k := range sortedReservoirs(in.Scents) {
				l.str(k, in.Scents[k])
			}
		}
	}
	return l.b.String()
}

func sortedReservoirs(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Numeric where they are numbers, alphabetical where they are not, so a
	// table nobody expected still comes out in a stable order.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && less(keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func less(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
