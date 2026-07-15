package convert

import "testing"

func TestExtension(t *testing.T) {
	cases := map[string]string{
		"slurm":    ".sh",
		"pbs":      ".sh",
		"lsf":      ".sh",
		"htcondor": ".sub",
		"flux":     ".json",
		"kueue":    ".yaml",
		"armada":   ".yaml",
		"splat":    ".yaml",
		"unknown":  ".yaml",
	}
	for format, want := range cases {
		if got := Extension(format); got != want {
			t.Errorf("Extension(%q): got %q want %q", format, got, want)
		}
	}
}
