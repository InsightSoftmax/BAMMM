package pbs_test

import (
	"os"
	"strings"
	"testing"
	"time"

	pbsemit "github.com/InsightSoftmax/BAMMM/internal/emitter/pbs"
	pbsparse "github.com/InsightSoftmax/BAMMM/internal/parser/pbs"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func TestEmit_Directives(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "j"},
		Spec: splat.Spec{
			Schedule: splat.Schedule{Queue: "gpu", Account: "acct", Walltime: splat.DurationOf(2 * time.Hour)},
			Resources: splat.Resources{
				Nodes: 2, CPUsPerTask: 8,
				MemoryPerTask: splat.QuantityOf(128 * 1024 * 1024 * 1024),
				GPU:           &splat.GPURequest{Count: 1},
			},
			Array:     &splat.Array{Indices: "0-9"},
			Execution: splat.Execution{Script: "#!/bin/bash\necho hi\n"},
		},
	}
	out, err := pbsemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"#PBS -N j",
		"#PBS -q gpu",
		"#PBS -A acct",
		"#PBS -J 0-9",
		"#PBS -l select=2:ncpus=8:mem=128gb:ngpus=1",
		"#PBS -l walltime=02:00:00",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing directive:\n  %s\n--- got ---\n%s", want, got)
		}
	}
}

// TestEmit_RoundTrip parses the reference PBS script, converts to SPLAT and
// back, and checks the key fields survive.
func TestEmit_RoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../conversions/03-htcondor-to-pbs/target.sh")
	if err != nil {
		t.Fatal(err)
	}
	first, err := pbsparse.Parse(data)
	if err != nil {
		t.Fatalf("Parse #1: %v", err)
	}
	out, err := pbsemit.Emit(first)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	second, err := pbsparse.Parse(out)
	if err != nil {
		t.Fatalf("Parse #2:\n%s\nerr: %v", out, err)
	}

	if second.Metadata.Name != first.Metadata.Name {
		t.Errorf("name drift: %q -> %q", first.Metadata.Name, second.Metadata.Name)
	}
	if second.Spec.Resources.CPUsPerTask != 8 {
		t.Errorf("cpus drift: got %d", second.Spec.Resources.CPUsPerTask)
	}
	if second.Spec.Resources.MemoryPerTask == nil || second.Spec.Resources.MemoryPerTask.String() != "128Gi" {
		t.Errorf("mem drift: got %v", second.Spec.Resources.MemoryPerTask)
	}
	if second.Spec.Resources.GPU == nil || second.Spec.Resources.GPU.Count != 1 {
		t.Errorf("gpu drift: got %v", second.Spec.Resources.GPU)
	}
	if second.Spec.Resources.DiskPerTask == nil || second.Spec.Resources.DiskPerTask.String() != "500Gi" {
		t.Errorf("scratch drift: got %v", second.Spec.Resources.DiskPerTask)
	}
	if second.Spec.Extensions.PBS["select_ib"] != "true" {
		t.Errorf("select_ib lost: got %v", second.Spec.Extensions.PBS["select_ib"])
	}
	if second.Spec.Array == nil || second.Spec.Array.Indices != "0-199" {
		t.Errorf("array drift: got %v", second.Spec.Array)
	}
}
