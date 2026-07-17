package kueue_test

import (
	"os"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"

	kueueemit "github.com/InsightSoftmax/BAMMM/internal/emitter/kueue"
	"github.com/InsightSoftmax/BAMMM/internal/jobset"
	slurmparse "github.com/InsightSoftmax/BAMMM/internal/parser/slurm"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// jobDoc extracts the batch/v1 Job document from a multi-doc YAML stream.
func jobDoc(t *testing.T, out []byte) *batchv1.Job {
	t.Helper()
	for _, doc := range strings.Split(string(out), "\n---\n") {
		if !strings.Contains(doc, "kind: Job") {
			continue
		}
		var j batchv1.Job
		if err := yaml.Unmarshal([]byte(doc), &j); err != nil {
			t.Fatalf("unmarshal Job doc: %v\n%s", err, doc)
		}
		return &j
	}
	t.Fatalf("no batch/v1 Job document found in output:\n%s", out)
	return nil
}

func TestEmit_SlurmToKueue(t *testing.T) {
	src, err := os.ReadFile("../../../conversions/01-slurm-to-volcano/source.sh")
	if err != nil {
		t.Fatal(err)
	}
	job, err := slurmparse.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := kueueemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	j := jobDoc(t, out)

	if j.Name != "bert-finetune" {
		t.Errorf("name: got %q", j.Name)
	}
	if got := j.Labels["kueue.x-k8s.io/queue-name"]; got != "gpu-hpc" {
		t.Errorf("queue-name label: got %q want gpu-hpc", got)
	}
	if got := j.Labels["bammm.io/source-format"]; got != "slurm" {
		t.Errorf("source-format label: got %q", got)
	}
	if j.Spec.ActiveDeadlineSeconds == nil || *j.Spec.ActiveDeadlineSeconds != 28800 {
		t.Errorf("activeDeadlineSeconds: got %v want 28800 (8h)", j.Spec.ActiveDeadlineSeconds)
	}
	if j.Spec.Parallelism == nil || *j.Spec.Parallelism != 32 {
		t.Errorf("parallelism: got %v want 32", j.Spec.Parallelism)
	}
	if j.Spec.Template.Spec.RestartPolicy != "Never" {
		t.Errorf("restartPolicy: got %q", j.Spec.Template.Spec.RestartPolicy)
	}

	c := j.Spec.Template.Spec.Containers[0]
	if cpu := c.Resources.Requests.Cpu().Value(); cpu != 6 {
		t.Errorf("cpu request: got %d want 6", cpu)
	}
	if mem := c.Resources.Requests.Memory().String(); mem != "48Gi" {
		t.Errorf("memory request: got %q want 48Gi (8Gi/cpu × 6)", mem)
	}
	gpu := c.Resources.Requests["nvidia.com/gpu"]
	if gpu.Value() != 2 {
		t.Errorf("gpu request: got %d want 2", gpu.Value())
	}
}

func TestEmit_ContainerPassthrough(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "infer"},
		Spec: splat.Spec{
			Schedule: splat.Schedule{Queue: "batch"},
			Execution: splat.Execution{
				Container: &splat.ContainerExecution{
					Image:   "myregistry/model:v1",
					Command: []string{"python", "serve.py"},
				},
			},
		},
	}
	out, err := kueueemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	j := jobDoc(t, out)
	c := j.Spec.Template.Spec.Containers[0]
	if c.Image != "myregistry/model:v1" {
		t.Errorf("image: got %q", c.Image)
	}
	// No ConfigMap should be emitted when a container image is present.
	if strings.Contains(string(out), "kind: ConfigMap") {
		t.Error("unexpected ConfigMap for container-based job")
	}
}

func TestEmit_MultiRoleJobSet(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "mc-sim"},
		Spec: splat.Spec{
			Schedule: splat.Schedule{Queue: "gpu"},
			Gang:     &splat.Gang{MinAvailable: 2},
			Tasks: []splat.Task{
				{
					Name:      "driver",
					Replicas:  1,
					Resources: &splat.Resources{CPUsPerTask: 4, GPU: &splat.GPURequest{Count: 1}},
					Execution: &splat.Execution{Container: &splat.ContainerExecution{Image: "sim:1", Command: []string{"drive"}}},
				},
				{
					Name:      "compute",
					Replicas:  4,
					Resources: &splat.Resources{CPUsPerTask: 32, GPU: &splat.GPURequest{Count: 4}},
					Execution: &splat.Execution{Container: &splat.ContainerExecution{Image: "sim:1", Command: []string{"work"}}},
				},
			},
		},
	}
	out, err := kueueemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(string(out), "kind: JobSet") {
		t.Fatalf("expected a JobSet for a multi-role job:\n%s", out)
	}

	var js jobset.JobSet
	for _, doc := range strings.Split(string(out), "\n---\n") {
		if strings.Contains(doc, "kind: JobSet") {
			if err := yaml.Unmarshal([]byte(doc), &js); err != nil {
				t.Fatalf("unmarshal JobSet: %v", err)
			}
		}
	}

	if js.Labels["kueue.x-k8s.io/queue-name"] != "gpu" {
		t.Errorf("queue-name label: got %q", js.Labels["kueue.x-k8s.io/queue-name"])
	}
	if len(js.Spec.ReplicatedJobs) != 2 {
		t.Fatalf("replicatedJobs: got %d want 2", len(js.Spec.ReplicatedJobs))
	}
	compute := js.Spec.ReplicatedJobs[1]
	if compute.Name != "compute" {
		t.Errorf("rj[1] name: got %q", compute.Name)
	}
	// A role with 4 pods → one Job replica with parallelism 4 (Indexed).
	if p := compute.Template.Spec.Parallelism; p == nil || *p != 4 {
		t.Errorf("compute parallelism: got %v want 4", p)
	}
	c := compute.Template.Spec.Template.Spec.Containers[0]
	if gpu := c.Resources.Requests["nvidia.com/gpu"]; gpu.Value() != 4 {
		t.Errorf("compute gpu: got %d want 4", gpu.Value())
	}
}

func TestEmit_NoWorkloadIsError(t *testing.T) {
	_, err := kueueemit.Emit(&splat.Job{Metadata: splat.Metadata{Name: "empty"}})
	if err == nil {
		t.Fatal("expected error for job with no workload")
	}
}
