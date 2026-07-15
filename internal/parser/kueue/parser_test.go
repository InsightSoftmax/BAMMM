package kueue_test

import (
	"strings"
	"testing"
	"time"

	"github.com/InsightSoftmax/BAMMM/internal/parser/kueue"
)

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
