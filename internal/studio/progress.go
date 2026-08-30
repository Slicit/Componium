package studio

import (
	"strings"
	"time"
)

// A progress bar that moves.
//
// The bar used to come from the composer, which reports a fraction as it
// passes each stage. That was fine while the stages were small and frequent
// and stopped being fine the moment the film was decoded once instead of five
// times: the composer now says five per cent, reads the whole film, and says
// forty-five. On a feature that is twenty minutes at five per cent, which
// looks exactly like a job that has hung.
//
// So the bar is predicted rather than reported. It does not have to be right —
// it has to move, because what it is actually answering is "is this still
// going", most often while a batch runs unattended.
//
// It predicts from the last time this film was analysed, which is the best
// estimate available and costs nothing: the steps of every past run are kept
// with the version it produced. Failing that, from the shape of a run in
// general, which is mostly decoding.

// share is roughly how much of a run each step is, when nothing better is
// known. Measured on crab rave through the studio: 1.3, 1.0, 24.0, 1.0, 1.1
// and 0.14 seconds, which is to say the decode and a rounding error.
var share = map[string]float64{
	"preparing":                   0.04,
	"measuring the audio":         0.04,
	"analysing":                   0.82,
	"joining the pieces":          0.04,
	"applying what the model saw": 0.04,
	"finding the quiet parts":     0.02,
}

// family groups the steps that are numbered, so "analysing 3 of 9" is looked
// up as "analysing".
func family(name string) string {
	if i := strings.Index(name, " "); i > 0 {
		if head := name[:i]; head == "analysing" {
			return head
		}
	}
	return name
}

// expect is how long each step of this film took last time, by name.
//
// Taken from the newest kept version that recorded any, which is the run
// before this one unless the history was cleared. A film analysed twice
// predicts itself well; a film analysed once falls back to the general shape.
func (j *Jobs) expect(film string) map[string]float64 {
	out := map[string]float64{}
	for _, v := range j.Versions(film) {
		if len(v.Steps) == 0 {
			continue
		}
		for _, s := range v.Steps {
			if s.Seconds > 0 {
				out[s.Name] = s.Seconds
			}
		}
		return out
	}
	return out
}

// predict turns the steps a run has recorded into a fraction of the whole.
//
// The finished steps are worth what they cost; the running one is worth its
// expected cost scaled by how long it has been going, capped just short of
// finishing so that a step running longer than expected creeps rather than
// overshoots. A bar that reaches the end and waits is worse than one that
// slows down, because the first looks stuck and the second looks slow.
// guessTotal is how long a whole run is expected to take, when this film has
// never been analysed before.
//
// Everything is a fraction of the film's own length, which is the only thing
// known about it up front. A fifth is loose — measured runs come in nearer an
// eighth since the film stopped being decoded five times — and loose is the
// right way to be wrong here: a bar that finishes early and waits looks stuck,
// and one that runs slow keeps moving.
const guessRatio = 0.2

func predict(steps []Step, expect map[string]float64, planned int, filmSeconds float64) float64 {
	if len(steps) == 0 {
		return 0
	}

	// Everything below is in seconds. It was not, once: the fallback returned
	// a share of the whole while elapsed time was measured in seconds, so
	// dividing one by the other saturated the running step within a second of
	// it starting and the bar sat still until the next one began. It read 17%
	// for the length of a decode.
	total := filmSeconds * guessRatio
	if total <= 0 {
		total = 120
	}

	cost := func(s Step) float64 {
		if v, ok := expect[s.Name]; ok && v > 0 {
			return v
		}
		if v, ok := share[family(s.Name)]; ok {
			if family(s.Name) == "analysing" && planned > 1 {
				return v * total / float64(planned)
			}
			return v * total
		}
		return 0.02 * total
	}

	// Everything a full run is expected to contain, not merely what has been
	// seen so far: a bar that only knows about the steps already started
	// reaches the end at the start of the last one.
	if len(expect) > 0 {
		total = 0
		for _, v := range expect {
			total += v
		}
		// A plan with more chunks than last time costs more than last time.
		if planned > 0 {
			seen := 0
			for name := range expect {
				if family(name) == "analysing" {
					seen++
				}
			}
			if seen > 0 && planned > seen {
				per := 0.0
				for name, v := range expect {
					if family(name) == "analysing" {
						per += v
					}
				}
				total += (per / float64(seen)) * float64(planned-seen)
			}
		}
	}
	if total <= 0 {
		return 0
	}

	done := 0.0
	for i, s := range steps {
		c := cost(s)
		if s.Seconds > 0 {
			done += c
			continue
		}
		if i != len(steps)-1 {
			// An unclosed step that is not the last one never finished; count
			// it as spent rather than pretending it is still running.
			done += c
			continue
		}
		if began, err := time.Parse(time.RFC3339, s.Started); err == nil && c > 0 {
			ran := time.Since(began).Seconds()
			f := ran / c
			if f > 0.95 {
				f = 0.95
			}
			done += c * f
		}
	}

	f := done / total
	if f > 0.99 {
		f = 0.99
	}
	if f < 0 {
		f = 0
	}
	return f
}

// filmSeconds is how much film a plan covers, which is the only thing known
// about a film that has never been analysed.
func filmSeconds(chunks []Chunk) float64 {
	if len(chunks) == 0 {
		return 0
	}
	return chunks[len(chunks)-1].To
}
