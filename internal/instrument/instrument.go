// Package instrument defines what the conductor knows about a device, which
// is deliberately very little.
//
// The conductor never learns what is on the other end of the wire. An
// instrument describes itself in a manifest, receives domain values rather
// than device commands, and is trusted to enforce its own limits. Translating
// a colour or a normalised intensity into DMX channels, PWM duty or a serial
// frame is the instrument's problem, and it is free to be ugly about it.
package instrument

import "time"

// Ramp is how long an effect takes to reach a commanded value once it has
// begun, which is distinct from Latency.
//
// Latency is dead time before anything at all happens. Ramp is the climb after
// it starts. A fan might have 1200ms of latency and then 1800ms of ramp, and a
// scheduler needs both to decide when a gust must begin in order to peak on
// the right frame.
type Ramp struct {
	Up   time.Duration
	Down time.Duration
}

// Manifest is an instrument's description of itself.
type Manifest struct {
	// ID is unique within a rig, conventionally kind.location, for example
	// "wind.main" or "light.ambient".
	ID string
	// Kind groups instruments that accept the same domain values.
	Kind string
	// Latency is the dead time between being told to act and the effect
	// beginning. The conductor dispatches every cue for this instrument this
	// much earlier, so an instrument that lies here is the easiest way to
	// make a rig feel wrong.
	Latency time.Duration
	// Ramp is declared but not yet used by the scheduler. Latency
	// compensation lands first; peak alignment needs the score format.
	Ramp Ramp
	// SafeState is what this instrument must be set to when anything goes
	// wrong: the conductor dies, the network drops, a limit is exceeded, or
	// someone hits all-stop. Fans to zero, foggers closed, a platform to
	// neutral.
	SafeState map[string]float64
	// DutyCycle is the fraction of any window this instrument may spend
	// active, 0 meaning no limit. A fogger that runs continuously empties
	// itself and sets off smoke alarms.
	DutyCycle float64
	// DutyWindow is the period DutyCycle is measured over. Zero means one
	// minute.
	DutyWindow time.Duration
	// MaxContinuous is how long this instrument may be driven without a rest.
	// Declared here, enforced by the instrument itself, never by the
	// conductor.
	MaxContinuous time.Duration
}

// Cue is one timed, discrete event.
type Cue struct {
	// At is when the effect should be perceived, in media time. It is not
	// when the cue is sent: the conductor subtracts the instrument's latency
	// to work that out.
	At time.Duration
	// Instrument is the ID this cue is addressed to.
	Instrument string
	// Action names what to do, interpreted by the instrument.
	Action string
	// Params carries domain values, in units the instrument's kind defines.
	Params map[string]float64
	// Hold is how long the effect should last. Zero means the cue is
	// momentary and needs no stopping: a flash, a single impact.
	//
	// A cue with a Hold becomes a span. The conductor schedules a matching
	// stop, and the instrument is *also* told the duration so that it ends
	// the effect itself. Both, deliberately. The stop is a UDP datagram and
	// can be lost; the conductor is a process and can crash. An instrument
	// that only stops when told is one dropped packet away from running
	// until somebody pulls a plug.
	Hold time.Duration
	// RequiredPrecision is how accurate the media clock must be for this cue
	// to be worth firing. Zero means no requirement.
	//
	// This is what stops a coarse time source silently ruining an effect that
	// depends on landing precisely. VLC's HTTP interface is good to about
	// 70ms, which is irrelevant to a fogger with seconds of its own lag and
	// fatal to a bass shaker.
	RequiredPrecision time.Duration
}

// Dispatch is a cue handed to an instrument, with the context it may need.
type Dispatch struct {
	Cue Cue
	// Media is the clock's media time at the moment of dispatch. It should be
	// close to Cue.At minus the instrument's latency.
	Media time.Duration
	// Wall is when the dispatch happened.
	Wall time.Time
	// Precision is the clock's precision at dispatch, so an instrument can
	// choose to soften an effect it knows is imprecisely timed.
	Precision time.Duration
}

// Instrument is a driver for one device.
type Instrument interface {
	Manifest() Manifest
	Dispatch(Dispatch) error
}

// ActionStop is the action a conductor sends to end a span.
//
// An instrument that does not recognise an action should do nothing, with one
// exception: this one. Failing to understand "stop" must never mean carrying
// on.
const ActionStop = "stop"

// IsStop reports whether an action ends an effect rather than starting one.
func IsStop(action string) bool {
	switch action {
	case ActionStop, "off", "safe", "neutral":
		return true
	}
	return false
}
