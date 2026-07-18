package runai_test

import (
	"testing"

	"sigs.k8s.io/yaml"

	raemit "github.com/InsightSoftmax/BAMMM/internal/emitter/runai"
	runaitypes "github.com/InsightSoftmax/BAMMM/internal/runai"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func TestEmit_FractionalGPU(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "train"},
		Spec: splat.Spec{
			Schedule:  splat.Schedule{Project: "team-a"},
			Resources: splat.Resources{CPUsPerTask: 4, GPU: &splat.GPURequest{Fraction: 0.5, Count: 1}},
			Execution: splat.Execution{Container: &splat.ContainerExecution{Image: "img:1", Command: []string{"python", "train.py"}}},
		},
	}
	out, err := raemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var w runaitypes.Workload
	if err := yaml.Unmarshal(out, &w); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if w.APIVersion != runaitypes.APIVersion || w.Kind != runaitypes.Kind {
		t.Errorf("type: %s/%s", w.APIVersion, w.Kind)
	}
	if w.Namespace != "runai-team-a" {
		t.Errorf("namespace: got %q want runai-team-a", w.Namespace)
	}
	if w.Spec.GPU == nil || w.Spec.GPU.Value != "0.5" {
		t.Errorf("gpu fraction: got %v want 0.5", w.Spec.GPU)
	}
	if w.Spec.Image == nil || w.Spec.Image.Value != "img:1" {
		t.Errorf("image: got %v", w.Spec.Image)
	}
	if w.Spec.CPU == nil || w.Spec.CPU.Value != "4" {
		t.Errorf("cpu: got %v", w.Spec.CPU)
	}
	if w.Spec.Command == nil || w.Spec.Command.Value != "python train.py" {
		t.Errorf("command: got %v", w.Spec.Command)
	}
}

func TestEmit_WholeGPU(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "w"},
		Spec: splat.Spec{
			Resources: splat.Resources{GPU: &splat.GPURequest{Count: 2}},
			Execution: splat.Execution{Container: &splat.ContainerExecution{Image: "i"}},
		},
	}
	out, _ := raemit.Emit(job)
	var w runaitypes.Workload
	_ = yaml.Unmarshal(out, &w)
	if w.Spec.GPU == nil || w.Spec.GPU.Value != "2" {
		t.Errorf("whole gpu: got %v want 2", w.Spec.GPU)
	}
}
