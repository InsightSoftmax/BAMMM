package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBytes(t *testing.T) {
	// Parses cleanly.
	if err := validateBytes([]byte(validSlurm), "slurm", ""); err != nil {
		t.Errorf("valid slurm: unexpected error %v", err)
	}
	// Also converts to a supported target.
	if err := validateBytes([]byte(validSlurm), "slurm", "kueue"); err != nil {
		t.Errorf("slurm->kueue: unexpected error %v", err)
	}
	// Fails to parse.
	if err := validateBytes([]byte("not a slurm script\n"), "slurm", ""); err == nil {
		t.Error("expected parse error for non-slurm input")
	}
	// Parses but target format is unknown → conversion error.
	if err := validateBytes([]byte(validSlurm), "slurm", "nonesuch"); err == nil {
		t.Error("expected error for unknown target format")
	}
}

func TestRunValidate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "good1.sh"), validSlurm)
	writeFile(t, filepath.Join(dir, "sub", "good2.sh"), validSlurm)
	writeFile(t, filepath.Join(dir, "bad.sh"), "not a slurm script\n")

	items, _, err := gatherInputs(nil, dir, "", true)
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	res := runValidate(items, "slurm", "", &buf)
	if res.valid != 2 {
		t.Errorf("valid=%d want 2", res.valid)
	}
	if res.invalid != 1 {
		t.Errorf("invalid=%d want 1", res.invalid)
	}
	out := buf.String()
	if !strings.Contains(out, "valid: 2/3") || !strings.Contains(out, "INVALID bad.sh") {
		t.Errorf("summary missing detail:\n%s", out)
	}
}
