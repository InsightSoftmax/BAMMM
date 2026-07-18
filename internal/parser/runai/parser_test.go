package runai_test

import (
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/parser/runai"
)

const workloadYAML = `
apiVersion: run.ai/v2alpha1
kind: TrainingWorkload
metadata:
  name: bert
  namespace: runai-nlp
spec:
  image:
    value: nvcr.io/pytorch:24.01
  gpu:
    value: "0.5"
  cpu:
    value: "2000m"
  memory:
    value: 8Gi
  command:
    value: python
  arguments:
    value: train.py
  parallelism:
    value: 4
`

func TestParse_Workload(t *testing.T) {
	job, err := runai.Parse([]byte(workloadYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Metadata.Name != "bert" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	// project recovered from runai-<project> namespace.
	if job.Spec.Schedule.Project != "nlp" {
		t.Errorf("project: got %q want nlp", job.Spec.Schedule.Project)
	}
	// fractional GPU: fraction kept, count rounded up to a whole GPU.
	g := job.Spec.Resources.GPU
	if g == nil || g.Fraction != 0.5 || g.Count != 1 {
		t.Errorf("gpu: got %+v want fraction 0.5 count 1", g)
	}
	if job.Spec.Resources.CPUsPerTask != 2 { // 2000m -> 2 cores
		t.Errorf("cpu: got %d want 2", job.Spec.Resources.CPUsPerTask)
	}
	if job.Spec.Resources.MemoryPerTask == nil || job.Spec.Resources.MemoryPerTask.String() != "8Gi" {
		t.Errorf("memory: got %v", job.Spec.Resources.MemoryPerTask)
	}
	if job.Spec.Resources.Tasks != 4 {
		t.Errorf("parallelism: got %d", job.Spec.Resources.Tasks)
	}
	c := job.Spec.Execution.Container
	if c == nil || c.Image != "nvcr.io/pytorch:24.01" {
		t.Fatalf("container: got %v", c)
	}
	if len(c.Command) != 1 || c.Command[0] != "python" || len(c.Args) != 1 || c.Args[0] != "train.py" {
		t.Errorf("command/args: got %v / %v", c.Command, c.Args)
	}
}
