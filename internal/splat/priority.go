package splat

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PriorityScale describes a scheduler's native priority convention so BAMMM can
// map it to and from SPLAT's canonical priority band: an integer 0–1000 where
// HIGHER ALWAYS MEANS HIGHER PRIORITY.
//
// This is the single place the direction problem is handled: schedulers disagree
// on both the numeric range (PBS -1024..1023, Slurm 0..2^32, …) and, for
// nice/fair-share style values, the direction (a *lower* native number meaning
// *higher* priority). Set Invert for those.
type PriorityScale struct {
	Min, Max int  // native range; values outside are clamped
	Invert   bool // true when a lower native value means higher priority (nice-like)
}

// Scheduler priority scales. Centralized so each format's parser and emitter
// agree on the same mapping. Ranges for unbounded schedulers (Slurm, HTCondor)
// are a practical band; native values beyond it clamp.
var (
	SlurmPriority    = PriorityScale{Min: 0, Max: 1000, Invert: false}
	PBSPriority      = PriorityScale{Min: -1024, Max: 1023, Invert: false}
	HTCondorPriority = PriorityScale{Min: -1000, Max: 1000, Invert: false}
	ArmadaPriority   = PriorityScale{Min: 0, Max: 1000, Invert: false}
)

// CanonicalScale is the interchange band that every native priority is mapped
// onto and back off of, with higher always meaning higher priority. It defaults
// to 0–1000; widening it (e.g. via `bammm convert --priority-range`) reduces the
// quantization loss when round-tripping schedulers with large native ranges.
// It is process-global: set it once before converting.
var CanonicalScale = PriorityScale{Min: 0, Max: 1000}

// Normalize maps a native priority value onto the canonical band (CanonicalScale).
func (s PriorityScale) Normalize(native int) int {
	if s.Max == s.Min {
		return CanonicalScale.Min
	}
	n := clampInt(native, s.Min, s.Max)
	frac := float64(n-s.Min) / float64(s.Max-s.Min) // 0..1, higher native → higher frac
	if s.Invert {
		frac = 1 - frac
	}
	return CanonicalScale.Min + int(math.Round(frac*float64(CanonicalScale.Max-CanonicalScale.Min)))
}

// Denormalize maps a canonical priority (on CanonicalScale) back to the
// scheduler's native range and direction. The result is always a valid native
// value.
func (s PriorityScale) Denormalize(canonical int) int {
	if CanonicalScale.Max == CanonicalScale.Min {
		return s.Min
	}
	c := clampInt(canonical, CanonicalScale.Min, CanonicalScale.Max)
	frac := float64(c-CanonicalScale.Min) / float64(CanonicalScale.Max-CanonicalScale.Min)
	if s.Invert {
		frac = 1 - frac
	}
	return s.Min + int(math.Round(frac*float64(s.Max-s.Min)))
}

// ParseRange parses a "MIN:MAX" priority band (e.g. "0:10000") into a
// non-inverted canonical PriorityScale. It errors on malformed input or when
// MAX does not exceed MIN.
func ParseRange(s string) (PriorityScale, error) {
	lo, hi, ok := strings.Cut(s, ":")
	if !ok {
		return PriorityScale{}, fmt.Errorf("priority range %q: expected MIN:MAX", s)
	}
	min, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return PriorityScale{}, fmt.Errorf("priority range %q: bad MIN: %w", s, err)
	}
	max, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil {
		return PriorityScale{}, fmt.Errorf("priority range %q: bad MAX: %w", s, err)
	}
	if max <= min {
		return PriorityScale{}, fmt.Errorf("priority range %q: MAX must exceed MIN", s)
	}
	return PriorityScale{Min: min, Max: max}, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
