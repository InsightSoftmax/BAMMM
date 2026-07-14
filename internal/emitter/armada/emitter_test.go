package armada_test

import (
	"os"
	"testing"

	armadatypes "github.com/InsightSoftmax/BAMMM/internal/armada"
	armadaemit "github.com/InsightSoftmax/BAMMM/internal/emitter/armada"
	armadaparse "github.com/InsightSoftmax/BAMMM/internal/parser/armada"

	"sigs.k8s.io/yaml"
)

// TestEmit_RoundTrip parses the reference Armada request, converts it to SPLAT
// and back, and verifies the reconstructed request preserves the key fields.
func TestEmit_RoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../conversions/05-armada-to-slurm/source.yaml")
	if err != nil {
		t.Fatal(err)
	}
	job, err := armadaparse.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := armadaemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var req armadatypes.Request
	if err := yaml.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal emitted request: %v\n%s", err, out)
	}

	if req.Queue != "research-simulations" {
		t.Errorf("queue: got %q", req.Queue)
	}
	if req.JobSetID != "mc-simulation-batch-2026-06" {
		t.Errorf("jobSetId: got %q", req.JobSetID)
	}
	if len(req.Jobs) != 2 {
		t.Fatalf("jobs: got %d want 2", len(req.Jobs))
	}

	driver := req.Jobs[0]
	if driver.Priority != 75 {
		t.Errorf("priority: got %v want 75", driver.Priority)
	}
	if driver.Namespace != "physics-team" {
		t.Errorf("namespace: got %q", driver.Namespace)
	}
	if driver.Labels["component"] != "driver" {
		t.Errorf("component label: got %q", driver.Labels["component"])
	}
	if driver.Annotations[armadatypes.AnnGangID] != "mc-sim-gang-001" {
		t.Errorf("gang-id: got %q", driver.Annotations[armadatypes.AnnGangID])
	}
	if driver.Annotations[armadatypes.AnnGangMinJobSize] != "2" {
		t.Errorf("gang-min: got %q", driver.Annotations[armadatypes.AnnGangMinJobSize])
	}
	if driver.PodSpec == nil {
		t.Fatal("driver podSpec nil")
	}
	if driver.PodSpec.PriorityClassName != "high" {
		t.Errorf("priorityClassName: got %q", driver.PodSpec.PriorityClassName)
	}
	if len(driver.PodSpec.Tolerations) != 1 {
		t.Errorf("tolerations: got %d want 1", len(driver.PodSpec.Tolerations))
	}
	c := driver.PodSpec.Containers[0]
	if c.Image != "cern/mc-simulator:3.2.1-cuda" {
		t.Errorf("image: got %q", c.Image)
	}
	if cpu := c.Resources.Requests.Cpu().Value(); cpu != 4 {
		t.Errorf("cpu request: got %d want 4", cpu)
	}
	gpu := c.Resources.Requests["nvidia.com/gpu"]
	if gpu.Value() != 1 {
		t.Errorf("gpu request: got %d want 1", gpu.Value())
	}

	// Ingress and services must survive the round-trip.
	if len(req.Ingress) != 1 || req.Ingress[0].Ports[0] != 9090 {
		t.Errorf("ingress not preserved: %+v", req.Ingress)
	}
	if len(req.Services) != 1 || req.Services[0].Name != "mc-sim-compute" {
		t.Errorf("services not preserved: %+v", req.Services)
	}
}
