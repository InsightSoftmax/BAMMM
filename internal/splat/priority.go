package splat

import "math"

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

// Normalize maps a native priority value onto the canonical 0–1000 band.
func (s PriorityScale) Normalize(native int) int {
	if s.Max == s.Min {
		return 0
	}
	n := clampInt(native, s.Min, s.Max)
	frac := float64(n-s.Min) / float64(s.Max-s.Min) // 0..1, higher native → higher frac
	if s.Invert {
		frac = 1 - frac
	}
	return int(math.Round(frac * 1000))
}

// Denormalize maps a canonical 0–1000 priority back to the scheduler's native
// range and direction. The result is always a valid native value.
func (s PriorityScale) Denormalize(canonical int) int {
	c := clampInt(canonical, 0, 1000)
	frac := float64(c) / 1000
	if s.Invert {
		frac = 1 - frac
	}
	return s.Min + int(math.Round(frac*float64(s.Max-s.Min)))
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
