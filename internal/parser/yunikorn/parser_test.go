package yunikorn_test

import (
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/parser/yunikorn"
)

const jobYAML = `
apiVersion: batch/v1
kind: Job
metadata:
  name: trainer
  namespace: ml
  labels:
    yunikorn.apache.org/app-id: trainer
    yunikorn.apache.org/queue: root.gpu
    team: nlp
spec:
  parallelism: 4
  backoffLimit: 2
  template:
    metadata:
      annotations:
        yunikorn.apache.org/task-group-name: members
        yunikorn.apache.org/task-groups: '[{"name":"members","minMember":4}]'
    spec:
      schedulerName: yunikorn
      containers:
        - name: main
          image: ubuntu:22.04
          command: ["/bin/bash", "-c"]
          args: ["echo hi"]
          resources:
            requests:
              cpu: "8"
              memory: 16Gi
`

func TestParse_Job(t *testing.T) {
	job, err := yunikorn.Parse([]byte(jobYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Metadata.Name != "trainer" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/namespace"] != "ml" {
		t.Errorf("namespace: got %q", job.Metadata.Annotations["bammm.io/namespace"])
	}
	// queue label loses the root. prefix.
	if job.Spec.Schedule.Queue != "gpu" {
		t.Errorf("queue: got %q want gpu", job.Spec.Schedule.Queue)
	}
	if job.Metadata.Labels["team"] != "nlp" {
		t.Errorf("user label: got %q", job.Metadata.Labels["team"])
	}
	if job.Spec.Resources.Tasks != 4 || job.Spec.Resources.CPUsPerTask != 8 {
		t.Errorf("resources: tasks=%d cpus=%d", job.Spec.Resources.Tasks, job.Spec.Resources.CPUsPerTask)
	}
	if job.Spec.Lifecycle.MaxRetries != 2 {
		t.Errorf("maxRetries: got %d", job.Spec.Lifecycle.MaxRetries)
	}
	// Inlined script recovered from the default-image /bin/bash -c container.
	if job.Spec.Execution.Script != "echo hi" {
		t.Errorf("script: got %q", job.Spec.Execution.Script)
	}
	// Gang recovered from the task-groups annotation.
	if job.Spec.Gang == nil || job.Spec.Gang.MinAvailable != 4 {
		t.Errorf("gang: got %v want minAvailable 4", job.Spec.Gang)
	}
}
