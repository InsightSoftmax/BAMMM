package slurm_test

import (
	"os"
	"strings"
	"testing"
	"time"

	slurmemit "github.com/InsightSoftmax/BAMMM/internal/emitter/slurm"
	slurmparse "github.com/InsightSoftmax/BAMMM/internal/parser/slurm"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestEmit_Source01_Directives checks that emitting the parsed reference script
// produces the expected #SBATCH directives.
func TestEmit_Source01_Directives(t *testing.T) {
	src := mustRead(t, "../../../conversions/01-slurm-to-volcano/source.sh")
	job, err := slurmparse.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := slurmemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := string(out)

	if !strings.HasPrefix(got, "#!/bin/bash\n") {
		t.Errorf("missing shebang; got start %q", got[:min(20, len(got))])
	}

	want := []string{
		"#SBATCH --job-name=bert-finetune",
		"#SBATCH --partition=gpu-hpc",
		"#SBATCH --account=nlp-research",
		"#SBATCH --qos=gpu-qos",
		"#SBATCH --nodes=4",
		"#SBATCH --ntasks-per-node=8",
		"#SBATCH --cpus-per-task=6",
		"#SBATCH --mem-per-cpu=8G",
		"#SBATCH --gres=gpu:a100:2",
		"#SBATCH --time=08:00:00",
		"#SBATCH --time-min=02:00:00",
		"#SBATCH --constraint=infiniband&avx512",
		"#SBATCH --output=/scratch/logs/bert-%j.out",
		"#SBATCH --error=/scratch/logs/bert-%j.err",
		"#SBATCH --mail-type=END,FAIL",
		"#SBATCH --mail-user=researcher@university.edu",
		"#SBATCH --signal=USR1@120",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing directive:\n  %s", w)
		}
	}

	// time_min and signal_before_end live in extensions.slurm but must NOT be
	// emitted twice (they are covered by --time-min / --signal above).
	if strings.Count(got, "--time-min") != 1 {
		t.Errorf("expected exactly one --time-min, got %d", strings.Count(got, "--time-min"))
	}
	if strings.Count(got, "--signal") != 1 {
		t.Errorf("expected exactly one --signal, got %d", strings.Count(got, "--signal"))
	}

	// The script body must be preserved.
	if !strings.Contains(got, "module load cuda/12.1") {
		t.Error("script body not preserved")
	}
}

// TestEmit_RoundTrip verifies slurm -> SPLAT -> slurm -> SPLAT is stable on the
// fields the parser understands.
func TestEmit_RoundTrip(t *testing.T) {
	src := mustRead(t, "../../../conversions/01-slurm-to-volcano/source.sh")
	first, err := slurmparse.Parse(src)
	if err != nil {
		t.Fatalf("Parse #1: %v", err)
	}
	out, err := slurmemit.Emit(first)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	second, err := slurmparse.Parse(out)
	if err != nil {
		t.Fatalf("Parse #2 of emitted output:\n%s\nerr: %v", out, err)
	}

	if second.Metadata.Name != first.Metadata.Name {
		t.Errorf("name drift: %q -> %q", first.Metadata.Name, second.Metadata.Name)
	}
	if second.Spec.Schedule.Partition != first.Spec.Schedule.Partition {
		t.Errorf("partition drift: %q -> %q", first.Spec.Schedule.Partition, second.Spec.Schedule.Partition)
	}
	if second.Spec.Schedule.Account != first.Spec.Schedule.Account {
		t.Errorf("account drift: %q -> %q", first.Spec.Schedule.Account, second.Spec.Schedule.Account)
	}
	if got, want := second.Spec.Schedule.Walltime.Duration(), 8*time.Hour; got != want {
		t.Errorf("walltime drift: got %v want %v", got, want)
	}
	if second.Spec.Resources.Nodes != first.Spec.Resources.Nodes {
		t.Errorf("nodes drift: %d -> %d", first.Spec.Resources.Nodes, second.Spec.Resources.Nodes)
	}
	if second.Spec.Resources.CPUsPerTask != first.Spec.Resources.CPUsPerTask {
		t.Errorf("cpus-per-task drift: %d -> %d", first.Spec.Resources.CPUsPerTask, second.Spec.Resources.CPUsPerTask)
	}
	if first.Spec.Resources.MemoryPerCPU == nil || second.Spec.Resources.MemoryPerCPU == nil {
		t.Fatal("mem-per-cpu lost in round-trip")
	}
	if got, want := second.Spec.Resources.MemoryPerCPU.Bytes(), first.Spec.Resources.MemoryPerCPU.Bytes(); got != want {
		t.Errorf("mem-per-cpu drift: got %d want %d bytes", got, want)
	}
	if first.Spec.Resources.GPU == nil || second.Spec.Resources.GPU == nil {
		t.Fatal("gpu lost in round-trip")
	}
	if got, want := second.Spec.Resources.GPU.Type, "a100"; got != want {
		t.Errorf("gpu type drift: got %q want %q", got, want)
	}
	if got, want := second.Spec.Resources.GPU.Count, 2.0; got != want {
		t.Errorf("gpu count drift: got %v want %v", got, want)
	}
}

func TestEmit_Dependencies(t *testing.T) {
	job := &splat.Job{
		Spec: splat.Spec{
			Dependencies: []splat.Dependency{
				{Scheme: splat.DepAfterOK, Value: "111"},
				{Scheme: splat.DepAfterOK, Value: "222"},
				{Scheme: splat.DepAfterAny, Value: "333"},
			},
		},
	}
	out, err := slurmemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(string(out), "--dependency=afterok:111:222,afterany:333") {
		t.Errorf("dependency grouping wrong:\n%s", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
