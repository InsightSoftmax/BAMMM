package yunikorn_test

import (
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/parser/yunikorn"
)

const jobSetYAML = `
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: gang-training
  namespace: default
  labels:
    yunikorn.apache.org/app-id: gang-training
    yunikorn.apache.org/queue: root.gpu
spec:
  replicatedJobs:
    - name: ps
      replicas: 1
      template:
        spec:
          template:
            metadata:
              annotations:
                yunikorn.apache.org/task-group-name: ps
                yunikorn.apache.org/task-groups: '[{"name":"ps","minMember":1},{"name":"worker","minMember":2}]'
            spec:
              schedulerName: yunikorn
              containers:
                - name: ps
                  image: tf:2.15
                  resources:
                    requests:
                      cpu: "2"
              restartPolicy: Never
    - name: worker
      replicas: 1
      template:
        spec:
          parallelism: 2
          completions: 2
          template:
            metadata:
              annotations:
                yunikorn.apache.org/task-group-name: worker
                yunikorn.apache.org/task-groups: '[{"name":"ps","minMember":1},{"name":"worker","minMember":2}]'
            spec:
              schedulerName: yunikorn
              containers:
                - name: worker
                  image: tf:2.15
                  resources:
                    requests:
                      cpu: "8"
                      nvidia.com/gpu: "1"
              restartPolicy: Never
`

func TestParse_JobSet_Gang(t *testing.T) {
	job, err := yunikorn.Parse([]byte(jobSetYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Schedule.Queue != "gpu" {
		t.Errorf("queue: got %q want gpu", job.Spec.Schedule.Queue)
	}
	if len(job.Spec.Tasks) != 2 {
		t.Fatalf("tasks: got %d want 2", len(job.Spec.Tasks))
	}
	if job.Spec.Tasks[0].Name != "ps" || job.Spec.Tasks[1].Name != "worker" {
		t.Errorf("task names: got %q, %q", job.Spec.Tasks[0].Name, job.Spec.Tasks[1].Name)
	}
	if job.Spec.Tasks[1].Replicas != 2 {
		t.Errorf("worker replicas: got %d want 2", job.Spec.Tasks[1].Replicas)
	}
	if job.Spec.Tasks[1].Resources == nil || job.Spec.Tasks[1].Resources.GPU == nil || job.Spec.Tasks[1].Resources.GPU.Count != 1 {
		t.Errorf("worker gpu: %+v", job.Spec.Tasks[1].Resources)
	}
	// Gang minAvailable is the sum of the task groups' minMembers (1 + 2).
	if job.Spec.Gang == nil || job.Spec.Gang.MinAvailable != 3 {
		t.Errorf("gang: %+v want MinAvailable=3", job.Spec.Gang)
	}
}

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
