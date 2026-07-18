package htcondor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/parser/htcondor"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func source(t *testing.T) *splat.Job {
	t.Helper()
	data, err := os.ReadFile("../../../conversions/03-htcondor-to-pbs/source.sub")
	if err != nil {
		t.Fatal(err)
	}
	job, err := htcondor.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return job
}

func TestParse_Core(t *testing.T) {
	job := source(t)
	if job.Metadata.Name != "genomics-sweep-2026-06" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/source-format"] != "htcondor" {
		t.Error("missing source-format annotation")
	}
	if job.Spec.Execution.Executable != "/scripts/run-variant-calling.sh" {
		t.Errorf("executable: got %q", job.Spec.Execution.Executable)
	}
	// docker universe + docker_image → container running the executable.
	c := job.Spec.Execution.Container
	if c == nil || c.Image != "broadinstitute/gatk:4.5.0.0" {
		t.Fatalf("container: got %v", c)
	}
	if job.Spec.Schedule.Account != "bio.variant-calling" || job.Spec.Schedule.Project != "genomics-pipeline" {
		t.Errorf("accounting: account=%q project=%q", job.Spec.Schedule.Account, job.Spec.Schedule.Project)
	}
}

func TestParse_Resources(t *testing.T) {
	r := source(t).Spec.Resources
	if r.CPUsPerTask != 8 {
		t.Errorf("cpus: got %d", r.CPUsPerTask)
	}
	if r.MemoryPerTask == nil || r.MemoryPerTask.String() != "32Gi" {
		t.Errorf("memory (32768M): got %v want 32Gi", r.MemoryPerTask)
	}
	if r.DiskPerTask == nil || r.DiskPerTask.String() != "200Gi" {
		t.Errorf("disk (200G): got %v want 200Gi", r.DiskPerTask)
	}
	if r.GPU == nil || r.GPU.Count != 1 {
		t.Errorf("gpu: got %v", r.GPU)
	}
}

func TestParse_ClassAdsPreserved(t *testing.T) {
	ext := source(t).Spec.Extensions.HTCondor
	if ext == nil {
		t.Fatal("no htcondor extensions")
	}
	for _, k := range []string{"requirements", "rank", "periodic_hold", "universe", "queue"} {
		if _, ok := ext[k]; !ok {
			t.Errorf("extension %q not preserved", k)
		}
	}
	req, _ := ext["requirements"].(string)
	if !strings.Contains(req, "HasInfiniband") {
		t.Errorf("requirements ClassAd lost: %q", req)
	}
}

func TestParse_BareNumberMemoryIsMiB(t *testing.T) {
	// request_memory with no suffix is MiB; request_disk is KiB.
	job, err := htcondor.Parse([]byte("executable = x\nrequest_memory = 2048\nrequest_disk = 1048576\nqueue\n"))
	if err != nil {
		t.Fatal(err)
	}
	if job.Spec.Resources.MemoryPerTask.String() != "2Gi" {
		t.Errorf("2048 MiB: got %v want 2Gi", job.Spec.Resources.MemoryPerTask)
	}
	if job.Spec.Resources.DiskPerTask.String() != "1Gi" {
		t.Errorf("1048576 KiB: got %v want 1Gi", job.Spec.Resources.DiskPerTask)
	}
}
