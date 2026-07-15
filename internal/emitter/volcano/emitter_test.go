package volcano_test

import (
	"os"
	"testing"

	"sigs.k8s.io/yaml"

	volcanoemit "github.com/InsightSoftmax/BAMMM/internal/emitter/volcano"
	volcanoparse "github.com/InsightSoftmax/BAMMM/internal/parser/volcano"
	volcanotypes "github.com/InsightSoftmax/BAMMM/internal/volcano"
)

// TestEmit_RoundTrip parses the reference vcjob, converts it to SPLAT and back,
// and checks the reconstructed vcjob preserves the key fields.
func TestEmit_RoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../conversions/02-volcano-to-slurm/source.yaml")
	if err != nil {
		t.Fatal(err)
	}
	job, err := volcanoparse.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := volcanoemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var vc volcanotypes.Job
	if err := yaml.Unmarshal(out, &vc); err != nil {
		t.Fatalf("unmarshal emitted vcjob: %v\n%s", err, out)
	}

	if vc.APIVersion != volcanotypes.APIVersion || vc.Kind != volcanotypes.Kind {
		t.Errorf("type meta: got %s/%s", vc.APIVersion, vc.Kind)
	}
	if vc.Spec.Queue != "ml-gpu" {
		t.Errorf("queue: got %q", vc.Spec.Queue)
	}
	if vc.Spec.MinAvailable != 7 {
		t.Errorf("minAvailable: got %d want 7", vc.Spec.MinAvailable)
	}
	if vc.Spec.SchedulerName != "volcano" {
		t.Errorf("schedulerName: got %q", vc.Spec.SchedulerName)
	}
	if vc.Spec.PriorityClassName != "high-priority" {
		t.Errorf("priorityClassName: got %q", vc.Spec.PriorityClassName)
	}
	if vc.Spec.MaxRetry != 3 {
		t.Errorf("maxRetry: got %d want 3", vc.Spec.MaxRetry)
	}
	if len(vc.Spec.Tasks) != 3 {
		t.Fatalf("tasks: got %d want 3", len(vc.Spec.Tasks))
	}

	worker := vc.Spec.Tasks[1]
	if worker.Name != "worker" || worker.Replicas != 4 {
		t.Errorf("worker: name=%q replicas=%d", worker.Name, worker.Replicas)
	}
	c := worker.Template.Spec.Containers[0]
	if c.Image != "tensorflow/tensorflow:2.14.0-gpu" {
		t.Errorf("worker image: got %q", c.Image)
	}
	if cpu := c.Resources.Requests.Cpu().Value(); cpu != 8 {
		t.Errorf("worker cpu: got %d want 8", cpu)
	}
	if len(worker.Template.Spec.Volumes) != 2 {
		t.Errorf("worker volumes: got %d want 2", len(worker.Template.Spec.Volumes))
	}
	if len(worker.Template.Spec.Tolerations) != 1 {
		t.Errorf("worker tolerations: got %d want 1", len(worker.Template.Spec.Tolerations))
	}
}
