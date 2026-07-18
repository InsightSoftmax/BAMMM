package kueue_test

import (
	"strings"
	"testing"
	"time"

	kueueemit "github.com/InsightSoftmax/BAMMM/internal/emitter/kueue"
	"github.com/InsightSoftmax/BAMMM/internal/parser/kueue"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// TestRoundTrip_MultiRoleJobSet guards the emitter and parser against drift: a
// multi-role job emitted as a JobSet must parse back to the same roles.
func TestRoundTrip_MultiRoleJobSet(t *testing.T) {
	orig := &splat.Job{
		APIVersion: splat.APIVersion,
		Kind:       splat.Kind,
		Metadata:   splat.Metadata{Name: "sim"},
		Spec: splat.Spec{
			Schedule: splat.Schedule{Queue: "gpu"},
			Tasks: []splat.Task{
				{
					Name:      "driver",
					Replicas:  1,
					Resources: &splat.Resources{CPUsPerTask: 4},
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

	out, err := kueueemit.Emit(orig)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	job, err := kueue.Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if job.Spec.Schedule.Queue != "gpu" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
	if len(job.Spec.Tasks) != 2 {
		t.Fatalf("tasks: got %d want 2", len(job.Spec.Tasks))
	}
	for i, want := range orig.Spec.Tasks {
		got := job.Spec.Tasks[i]
		if got.Name != want.Name || got.Replicas != want.Replicas {
			t.Errorf("task %d: got %s/%d want %s/%d", i, got.Name, got.Replicas, want.Name, want.Replicas)
		}
		if got.Execution == nil || got.Execution.Container == nil || got.Execution.Container.Image != "sim:1" {
			t.Errorf("task %d execution: %+v", i, got.Execution)
		}
	}
	if job.Spec.Tasks[1].Resources == nil || job.Spec.Tasks[1].Resources.GPU == nil || job.Spec.Tasks[1].Resources.GPU.Count != 4 {
		t.Errorf("compute gpu: %+v", job.Spec.Tasks[1].Resources)
	}
}

const scriptJob = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: bert-finetune-script
  namespace: research
data:
  job.sh: |
    #!/bin/bash
    echo hello
    srun python train.py
---
apiVersion: batch/v1
kind: Job
metadata:
  name: bert-finetune
  namespace: research
  labels:
    kueue.x-k8s.io/queue-name: gpu-hpc
    team: nlp
spec:
  parallelism: 8
  completions: 8
  activeDeadlineSeconds: 3600
  backoffLimit: 2
  template:
    spec:
      nodeSelector:
        disktype: ssd
      containers:
        - name: main
          image: ubuntu:22.04
          command: ["/bin/bash", "/bammm/job.sh"]
          env:
            - name: OMP_NUM_THREADS
              value: "6"
          resources:
            requests:
              cpu: "6"
              memory: 48Gi
              nvidia.com/gpu: "2"
          volumeMounts:
            - name: bammm-script
              mountPath: /bammm
      volumes:
        - name: bammm-script
          configMap:
            name: bert-finetune-script
      restartPolicy: Never
`

const jobSetManifest = `
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: ml-training
  namespace: research
  labels:
    kueue.x-k8s.io/queue-name: gpu-hpc
    team: nlp
spec:
  replicatedJobs:
    - name: driver
      replicas: 1
      template:
        spec:
          backoffLimit: 3
          template:
            spec:
              containers:
                - name: driver
                  image: ray:2.9
                  command: ["ray", "start", "--head"]
                  resources:
                    requests:
                      cpu: "4"
                      memory: 8Gi
              restartPolicy: Never
    - name: worker
      replicas: 1
      template:
        spec:
          parallelism: 3
          completions: 3
          backoffLimit: 3
          template:
            spec:
              nodeSelector:
                gpu: "true"
              containers:
                - name: worker
                  image: ubuntu:22.04
                  command: ["/bin/bash", "-c"]
                  args: ["echo hi && sleep 1"]
                  resources:
                    requests:
                      cpu: "8"
                      memory: 16Gi
                      nvidia.com/gpu: "4"
              restartPolicy: Never
`

func TestParse_JobSet(t *testing.T) {
	job, err := kueue.Parse([]byte(jobSetManifest))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if job.Metadata.Name != "ml-training" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/namespace"] != "research" {
		t.Errorf("namespace: got %q", job.Metadata.Annotations["bammm.io/namespace"])
	}
	if job.Spec.Schedule.Queue != "gpu-hpc" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
	if job.Metadata.Labels["team"] != "nlp" {
		t.Errorf("label team: got %q", job.Metadata.Labels["team"])
	}
	if job.Spec.Lifecycle.MaxRetries != 3 {
		t.Errorf("maxRetries: got %d want 3", job.Spec.Lifecycle.MaxRetries)
	}

	if len(job.Spec.Tasks) != 2 {
		t.Fatalf("tasks: got %d want 2", len(job.Spec.Tasks))
	}

	driver := job.Spec.Tasks[0]
	if driver.Name != "driver" || driver.Replicas != 1 {
		t.Errorf("driver: name=%q replicas=%d", driver.Name, driver.Replicas)
	}
	if driver.Execution == nil || driver.Execution.Container == nil || driver.Execution.Container.Image != "ray:2.9" {
		t.Errorf("driver execution: %+v", driver.Execution)
	}
	if driver.Resources == nil || driver.Resources.CPUsPerTask != 4 {
		t.Errorf("driver cpus: %+v", driver.Resources)
	}

	worker := job.Spec.Tasks[1]
	if worker.Name != "worker" || worker.Replicas != 3 {
		t.Errorf("worker: name=%q replicas=%d want worker/3", worker.Name, worker.Replicas)
	}
	// An inlined /bin/bash -c on the placeholder image round-trips to a script.
	if worker.Execution == nil || worker.Execution.Container != nil {
		t.Errorf("worker should be a script, got %+v", worker.Execution)
	}
	if worker.Execution.Script != "echo hi && sleep 1" {
		t.Errorf("worker script: got %q", worker.Execution.Script)
	}
	if worker.Resources == nil || worker.Resources.GPU == nil || worker.Resources.GPU.Count != 4 {
		t.Errorf("worker gpu: %+v", worker.Resources)
	}
	if worker.Placement == nil || worker.Placement.NodeSelector["gpu"] != "true" {
		t.Errorf("worker nodeSelector: %+v", worker.Placement)
	}
}

func TestParse_ScriptJob(t *testing.T) {
	job, err := kueue.Parse([]byte(scriptJob))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if job.Metadata.Name != "bert-finetune" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/source-format"] != "kueue" {
		t.Error("missing source-format annotation")
	}
	if job.Metadata.Annotations["bammm.io/namespace"] != "research" {
		t.Errorf("namespace annotation: got %q", job.Metadata.Annotations["bammm.io/namespace"])
	}
	if job.Spec.Schedule.Queue != "gpu-hpc" {
		t.Errorf("queue: got %q", job.Spec.Schedule.Queue)
	}
	if job.Metadata.Labels["team"] != "nlp" {
		t.Errorf("label team: got %q", job.Metadata.Labels["team"])
	}
	if job.Spec.Resources.Tasks != 8 {
		t.Errorf("tasks: got %d want 8", job.Spec.Resources.Tasks)
	}
	if job.Spec.Schedule.Walltime == nil || job.Spec.Schedule.Walltime.Duration() != time.Hour {
		t.Errorf("walltime: got %v want 1h", job.Spec.Schedule.Walltime)
	}
	if job.Spec.Lifecycle.MaxRetries != 2 {
		t.Errorf("maxRetries: got %d want 2", job.Spec.Lifecycle.MaxRetries)
	}
	if job.Spec.Resources.CPUsPerTask != 6 {
		t.Errorf("cpus: got %d want 6", job.Spec.Resources.CPUsPerTask)
	}
	if job.Spec.Resources.MemoryPerTask == nil || job.Spec.Resources.MemoryPerTask.String() != "48Gi" {
		t.Errorf("memory: got %v want 48Gi", job.Spec.Resources.MemoryPerTask)
	}
	if job.Spec.Resources.GPU == nil || job.Spec.Resources.GPU.Count != 2 {
		t.Errorf("gpu: got %v want 2", job.Spec.Resources.GPU)
	}
	if job.Spec.Placement.NodeSelector["disktype"] != "ssd" {
		t.Errorf("nodeSelector: got %v", job.Spec.Placement.NodeSelector)
	}

	// Script must be recovered from the ConfigMap, not left as a container.
	if job.Spec.Execution.Container != nil {
		t.Error("expected script execution, got container")
	}
	if got := job.Spec.Execution.Script; got == "" || !strings.Contains(got, "srun python train.py") {
		t.Errorf("script not recovered: %q", got)
	}
	if job.Spec.Execution.Environment.Vars["OMP_NUM_THREADS"] != "6" {
		t.Errorf("env not parsed: %v", job.Spec.Execution.Environment.Vars)
	}
}
