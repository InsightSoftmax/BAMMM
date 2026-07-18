package volcano_test

import (
	"os"
	"testing"
	"time"

	"github.com/InsightSoftmax/BAMMM/internal/parser/volcano"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func source(t *testing.T) *splat.Job {
	t.Helper()
	data, err := os.ReadFile("../../../conversions/02-volcano-to-slurm/source.yaml")
	if err != nil {
		t.Fatal(err)
	}
	job, err := volcano.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return job
}

func TestParse_Envelope(t *testing.T) {
	job := source(t)
	if job.Metadata.Name != "tf-resnet-training" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/namespace"] != "ml-team" {
		t.Errorf("namespace: got %q", job.Metadata.Annotations["bammm.io/namespace"])
	}
	if job.Spec.Schedule.Queue != "ml-gpu" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
	if job.Spec.Schedule.PriorityClass != "high-priority" {
		t.Errorf("priorityClass: got %q", job.Spec.Schedule.PriorityClass)
	}
	if job.Spec.Lifecycle.MaxRetries != 3 {
		t.Errorf("maxRetry: got %d want 3", job.Spec.Lifecycle.MaxRetries)
	}
	if ttl := job.Spec.Lifecycle.TTLAfterFinished; ttl == nil || ttl.Duration() != 2*time.Hour {
		t.Errorf("ttl: got %v want 2h", ttl)
	}
}

func TestParse_GangAndTasks(t *testing.T) {
	job := source(t)
	if job.Spec.Gang == nil || job.Spec.Gang.MinAvailable != 7 {
		t.Fatalf("gang: got %v want minAvailable 7", job.Spec.Gang)
	}
	if len(job.Spec.Tasks) != 3 {
		t.Fatalf("tasks: got %d want 3", len(job.Spec.Tasks))
	}
	worker := job.Spec.Tasks[1]
	if worker.Name != "worker" || worker.Replicas != 4 {
		t.Errorf("worker: got name=%q replicas=%d", worker.Name, worker.Replicas)
	}
	if worker.Resources.GPU == nil || worker.Resources.GPU.Count != 4 {
		t.Errorf("worker gpu: got %v want 4", worker.Resources.GPU)
	}
	ps := job.Spec.Tasks[2]
	if ps.Resources.GPU != nil {
		t.Errorf("ps should have no gpu, got %v", ps.Resources.GPU)
	}
	if ps.Resources.MemoryPerTask == nil || ps.Resources.MemoryPerTask.String() != "16Gi" {
		t.Errorf("ps mem: got %v want 16Gi", ps.Resources.MemoryPerTask)
	}
}

func TestParse_Volumes(t *testing.T) {
	job := source(t)
	// Two PVCs are shared across all three task templates; they should be
	// deduplicated to two job-level volumes.
	if len(job.Spec.Volumes) != 2 {
		t.Fatalf("volumes: got %d want 2", len(job.Spec.Volumes))
	}
	byName := map[string]splat.Volume{}
	for _, v := range job.Spec.Volumes {
		byName[v.Name] = v
	}
	if byName["data"].PVC != "imagenet-pvc" {
		t.Errorf("data volume PVC: got %q", byName["data"].PVC)
	}
	if byName["models"].MountPath != "/models" {
		t.Errorf("models mountPath: got %q", byName["models"].MountPath)
	}
}

func TestParse_MultiDoc(t *testing.T) {
	// A real-world bundle: Namespace + Queue + the vcjob. The parser must find
	// the batch.volcano.sh Job among the other documents.
	bundle := []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: ml-team
---
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-gpu
spec:
  weight: 1
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: trainer
spec:
  minAvailable: 1
  queue: ml-gpu
  tasks:
    - name: worker
      replicas: 2
      template:
        spec:
          containers:
            - name: c
              image: trainer:1
`)
	job, err := volcano.Parse(bundle)
	if err != nil {
		t.Fatalf("Parse multi-doc: %v", err)
	}
	if job.Metadata.Name != "trainer" {
		t.Errorf("name: got %q want trainer", job.Metadata.Name)
	}
	if len(job.Spec.Tasks) != 1 || job.Spec.Tasks[0].Name != "worker" {
		t.Errorf("tasks: got %v", job.Spec.Tasks)
	}
	if job.Spec.Schedule.Queue != "ml-gpu" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
}

func TestParse_Extensions(t *testing.T) {
	job := source(t)
	ext := job.Spec.Extensions.Volcano
	if ext == nil {
		t.Fatal("volcano extensions missing")
	}
	if ext["scheduler_name"] != "volcano" {
		t.Errorf("scheduler_name: got %v", ext["scheduler_name"])
	}
	if ext["plugins"] == nil {
		t.Error("plugins not stashed")
	}
	if ext["policies"] == nil {
		t.Error("policies not stashed")
	}
}
