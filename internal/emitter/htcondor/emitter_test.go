package htcondor_test

import (
	"os"
	"strings"
	"testing"

	htcemit "github.com/InsightSoftmax/BAMMM/internal/emitter/htcondor"
	htcparse "github.com/InsightSoftmax/BAMMM/internal/parser/htcondor"
)

func TestEmit_RoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../conversions/03-htcondor-to-pbs/source.sub")
	if err != nil {
		t.Fatal(err)
	}
	first, err := htcparse.Parse(data)
	if err != nil {
		t.Fatalf("Parse #1: %v", err)
	}
	out, err := htcemit.Emit(first)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		"universe = docker",
		"docker_image = broadinstitute/gatk:4.5.0.0",
		"batch_name = genomics-sweep-2026-06",
		"request_cpus = 8",
		"request_memory = 32G",
		"request_gpus = 1",
		"+ProjectName = genomics-pipeline",
		"+AccountingGroup = bio.variant-calling",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing:\n  %s", want)
		}
	}

	// ClassAd requirements must survive, and the queue statement must be last.
	if !strings.Contains(got, "requirements = ") || !strings.Contains(got, "HasInfiniband") {
		t.Error("requirements ClassAd not emitted")
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "from /scratch/sample-manifest.txt") {
		t.Errorf("queue statement not last:\n%s", got)
	}

	// Re-parse to confirm the round-trip is stable.
	second, err := htcparse.Parse(out)
	if err != nil {
		t.Fatalf("Parse #2: %v", err)
	}
	if second.Spec.Resources.CPUsPerTask != 8 {
		t.Errorf("cpus drift: %d", second.Spec.Resources.CPUsPerTask)
	}
	if second.Spec.Execution.Container == nil || second.Spec.Execution.Container.Image != "broadinstitute/gatk:4.5.0.0" {
		t.Errorf("image drift: %v", second.Spec.Execution.Container)
	}
}
