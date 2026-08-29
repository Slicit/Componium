package cip

import "math"

// Thin wrappers so the wire format code reads without math. prefixes cluttering
// the layout, which is the part worth being able to check against the spec.
func mathFloat32bits(f float32) uint32     { return math.Float32bits(f) }
func mathFloat32frombits(u uint32) float32 { return math.Float32frombits(u) }
