package splat

import "testing"

func TestPriorityScale_Canonical(t *testing.T) {
	// Higher native → higher canonical for a non-inverted scale.
	s := PriorityScale{Min: 0, Max: 1000}
	if got := s.Normalize(700); got != 700 {
		t.Errorf("Normalize(700): got %d want 700", got)
	}
	if got := s.Denormalize(700); got != 700 {
		t.Errorf("Denormalize(700): got %d want 700", got)
	}
}

func TestPriorityScale_Clamp(t *testing.T) {
	// Out-of-range native values clamp; Denormalize never leaves the range.
	if got := PBSPriority.Normalize(1_000_000); got != 1000 {
		t.Errorf("above max: got %d want 1000", got)
	}
	if got := PBSPriority.Normalize(-1_000_000); got != 0 {
		t.Errorf("below min: got %d want 0", got)
	}
	if got := PBSPriority.Denormalize(1000); got != 1023 {
		t.Errorf("Denormalize(1000) for PBS: got %d want 1023 (native max)", got)
	}
	if got := PBSPriority.Denormalize(0); got != -1024 {
		t.Errorf("Denormalize(0) for PBS: got %d want -1024 (native min)", got)
	}
}

func TestPriorityScale_Invert(t *testing.T) {
	// nice-style: a LOWER native value means HIGHER priority. Highest priority
	// (min native) must map to the top of the canonical band, and vice versa.
	nice := PriorityScale{Min: -20, Max: 19, Invert: true}
	if got := nice.Normalize(-20); got != 1000 {
		t.Errorf("nice -20 (highest): got %d want 1000", got)
	}
	if got := nice.Normalize(19); got != 0 {
		t.Errorf("nice 19 (lowest): got %d want 0", got)
	}
	// Round-trips back to the same native direction.
	if got := nice.Denormalize(1000); got != -20 {
		t.Errorf("Denormalize(1000): got %d want -20", got)
	}
	if got := nice.Denormalize(0); got != 19 {
		t.Errorf("Denormalize(0): got %d want 19", got)
	}
}

func TestParseRange(t *testing.T) {
	s, err := ParseRange("0:10000")
	if err != nil || s.Min != 0 || s.Max != 10000 {
		t.Fatalf("ParseRange(0:10000): got %+v, %v", s, err)
	}
	if s, err := ParseRange(" -5 : 5 "); err != nil || s.Min != -5 || s.Max != 5 {
		t.Errorf("ParseRange with spaces: got %+v, %v", s, err)
	}
	for _, bad := range []string{"", "0", "5:5", "10:2", "a:b", "0:x"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q): expected error", bad)
		}
	}
}

func TestPriority_WideCanonicalImprovesRoundTrip(t *testing.T) {
	orig := CanonicalScale
	defer func() { CanonicalScale = orig }()

	// PBS has 2048 distinct native values; the default 0–1000 band cannot
	// represent them all, so some native→canonical→native round-trips lose
	// precision. Count how many.
	lossy := func() int {
		n := 0
		for v := PBSPriority.Min; v <= PBSPriority.Max; v++ {
			if PBSPriority.Denormalize(PBSPriority.Normalize(v)) != v {
				n++
			}
		}
		return n
	}

	CanonicalScale = PriorityScale{Min: 0, Max: 1000}
	if lossy() == 0 {
		t.Fatal("expected the default 0–1000 band to lose precision across PBS's range")
	}

	// A band at least as wide as the native range makes the round-trip lossless.
	CanonicalScale = PriorityScale{Min: 0, Max: 100000}
	if n := lossy(); n != 0 {
		t.Errorf("wide band should be lossless, got %d lossy values", n)
	}
}

func TestPriorityScale_DirectionAgreement(t *testing.T) {
	// Two schedulers with opposite native directions must agree on canonical
	// ordering: the higher-priority job stays higher-priority after conversion.
	higherIsBetter := PriorityScale{Min: 0, Max: 100}
	lowerIsBetter := PriorityScale{Min: 0, Max: 100, Invert: true}

	// A high-priority job in each native scheme:
	canonHigh1 := higherIsBetter.Normalize(90) // near the top
	canonHigh2 := lowerIsBetter.Normalize(10)  // near the bottom = high priority
	if canonHigh1 < 500 || canonHigh2 < 500 {
		t.Errorf("both should be high canonical priority: %d, %d", canonHigh1, canonHigh2)
	}
}
