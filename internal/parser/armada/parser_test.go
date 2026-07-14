package armada_test

import (
	"os"
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/parser/armada"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func source(t *testing.T) *splat.Job {
	t.Helper()
	data, err := os.ReadFile("../../../conversions/05-armada-to-slurm/source.yaml")
	if err != nil {
		t.Fatal(err)
	}
	job, err := armada.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return job
}

func TestParse_Envelope(t *testing.T) {
	job := source(t)

	if job.Metadata.Name != "mc-simulation-batch-2026-06" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Spec.Schedule.Queue != "research-simulations" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
	if job.Spec.Schedule.PriorityClass != "high" {
		t.Errorf("priorityClass: got %q", job.Spec.Schedule.PriorityClass)
	}
	if job.Metadata.Annotations["bammm.io/source-format"] != "armada" {
		t.Error("missing source-format annotation")
	}
	if job.Metadata.Annotations["bammm.io/external-uri"] == "" {
		t.Error("external-uri not captured")
	}
	// component label differs per job → dropped; common ones retained.
	if job.Metadata.Labels["component"] != "" {
		t.Errorf("component label should be dropped from job metadata, got %q", job.Metadata.Labels["component"])
	}
	if job.Metadata.Labels["app"] != "monte-carlo-sim" {
		t.Errorf("common label app: got %q", job.Metadata.Labels["app"])
	}
}

func TestParse_Tasks(t *testing.T) {
	job := source(t)

	if len(job.Spec.Tasks) != 2 {
		t.Fatalf("tasks: got %d want 2", len(job.Spec.Tasks))
	}
	driver := job.Spec.Tasks[0]
	if driver.Name != "driver" {
		t.Errorf("task[0] name: got %q", driver.Name)
	}
	if driver.Resources.CPUsPerTask != 4 {
		t.Errorf("driver cpu: got %d want 4", driver.Resources.CPUsPerTask)
	}
	if driver.Resources.GPU == nil || driver.Resources.GPU.Count != 1 {
		t.Errorf("driver gpu: got %v want 1", driver.Resources.GPU)
	}
	if driver.Execution.Container.Image != "cern/mc-simulator:3.2.1-cuda" {
		t.Errorf("driver image: got %q", driver.Execution.Container.Image)
	}
	if driver.Execution.Environment.Vars["CUDA_VISIBLE_DEVICES"] != "0" {
		t.Errorf("driver env: got %v", driver.Execution.Environment.Vars)
	}
	if driver.Placement == nil || len(driver.Placement.Tolerations) != 1 {
		t.Errorf("driver tolerations: got %v", driver.Placement)
	}

	compute := job.Spec.Tasks[1]
	if compute.Resources.CPUsPerTask != 32 {
		t.Errorf("compute cpu: got %d want 32", compute.Resources.CPUsPerTask)
	}
	if compute.Resources.MemoryPerTask == nil || compute.Resources.MemoryPerTask.String() != "128Gi" {
		t.Errorf("compute mem: got %v want 128Gi", compute.Resources.MemoryPerTask)
	}
}

func TestParse_Gang(t *testing.T) {
	job := source(t)
	if job.Spec.Gang == nil {
		t.Fatal("gang not derived")
	}
	if job.Spec.Gang.MinAvailable != 2 {
		t.Errorf("gang minAvailable: got %d want 2", job.Spec.Gang.MinAvailable)
	}
	if job.Spec.Gang.Style != splat.GangStyleHard {
		t.Errorf("gang style: got %q want hard", job.Spec.Gang.Style)
	}
}

func TestParse_Extensions(t *testing.T) {
	job := source(t)
	ext := job.Spec.Extensions.Armada
	if ext == nil {
		t.Fatal("armada extensions missing")
	}
	if ext["job_set_id"] != "mc-simulation-batch-2026-06" {
		t.Errorf("job_set_id: got %v", ext["job_set_id"])
	}
	if ext["namespace"] != "physics-team" {
		t.Errorf("namespace: got %v", ext["namespace"])
	}
	if ext["ingress"] == nil {
		t.Error("ingress not stashed")
	}
	if ext["services"] == nil {
		t.Error("services not stashed")
	}
}
