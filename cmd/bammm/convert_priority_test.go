package main

import (
	"io"
	"strings"
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func runConvert(t *testing.T, in string, args ...string) error {
	t.Helper()
	cmd := newConvertCmd()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(in))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

func TestConvert_PriorityRangeFlag(t *testing.T) {
	orig := splat.CanonicalScale
	defer func() { splat.CanonicalScale = orig }()

	// A malformed range is rejected before any conversion runs.
	if err := runConvert(t, validSlurm, "--from", "slurm", "--to", "pbs", "--priority-range", "10:2"); err == nil {
		t.Fatal("expected an error for MAX <= MIN")
	}
	if err := runConvert(t, validSlurm, "--from", "slurm", "--to", "pbs", "--priority-range", "oops"); err == nil {
		t.Fatal("expected an error for a non-numeric range")
	}

	// A valid range is applied to the process-global canonical scale.
	if err := runConvert(t, validSlurm, "--from", "slurm", "--to", "pbs", "--priority-range", "0:5000"); err != nil {
		t.Fatalf("convert with valid range: %v", err)
	}
	if splat.CanonicalScale.Min != 0 || splat.CanonicalScale.Max != 5000 {
		t.Errorf("canonical scale not applied: got %+v want {0 5000}", splat.CanonicalScale)
	}

	// Omitting the flag restores the 0–1000 default.
	if err := runConvert(t, validSlurm, "--from", "slurm", "--to", "pbs"); err != nil {
		t.Fatalf("convert with default range: %v", err)
	}
	if splat.CanonicalScale.Min != 0 || splat.CanonicalScale.Max != 1000 {
		t.Errorf("default scale: got %+v want {0 1000}", splat.CanonicalScale)
	}
}
