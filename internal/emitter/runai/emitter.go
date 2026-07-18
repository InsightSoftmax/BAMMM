// Package runai emits Run.ai (NVIDIA) TrainingWorkload manifests from SPLAT jobs.
// It is the inverse of internal/parser/runai. Run.ai's fractional GPU is the one
// place SPLAT's GPURequest.Fraction is used directly.
package runai

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	runaitypes "github.com/InsightSoftmax/BAMMM/internal/runai"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("runai", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// Emit converts a SPLAT job into a Run.ai TrainingWorkload.
func Emit(job *splat.Job) ([]byte, error) {
	name := job.Metadata.Name
	if name == "" {
		name = "bammm-job"
	}
	exec, res := primaryWorkload(job)
	if exec == nil {
		return nil, fmt.Errorf("runai: job has no execution to run")
	}

	w := &runaitypes.Workload{
		TypeMeta: metav1.TypeMeta{APIVersion: runaitypes.APIVersion, Kind: runaitypes.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespaceFor(job),
			Labels:    job.Metadata.Labels,
		},
	}

	image, command, args := workloadCommand(exec)
	if image != "" {
		w.Spec.Image = str(image)
	}
	if command != "" {
		w.Spec.Command = str(command)
	}
	if args != "" {
		w.Spec.Arguments = str(args)
	}

	if res != nil {
		if res.CPUsPerTask > 0 {
			w.Spec.CPU = str(strconv.Itoa(res.CPUsPerTask))
		}
		if res.MemoryPerTask != nil {
			w.Spec.Memory = str(res.MemoryPerTask.String())
		}
		if g := res.GPU; g != nil {
			if gv := gpuValue(g); gv != "" {
				w.Spec.GPU = str(gv)
			}
			if g.Memory != nil {
				w.Spec.GPUMemory = str(g.Memory.String())
			}
		}
		if res.Tasks > 1 {
			w.Spec.Parallelism = &runaitypes.IntValue{Value: res.Tasks}
			w.Spec.Completions = &runaitypes.IntValue{Value: res.Tasks}
		}
	}

	if env := envItems(exec); env != nil {
		w.Spec.Environment = env
	}

	out, err := k8senc.MarshalClean(w)
	if err != nil {
		return nil, fmt.Errorf("runai: marshal: %w", err)
	}
	return out, nil
}

// gpuValue renders the GPU request: a fraction ("0.5") when set, else a whole
// count. Run.ai is the only target that keeps the fraction.
func gpuValue(g *splat.GPURequest) string {
	if g.Fraction > 0 && g.Fraction < 1 {
		return strconv.FormatFloat(g.Fraction, 'g', -1, 64)
	}
	if g.Count > 0 {
		if g.Count == math.Trunc(g.Count) {
			return strconv.Itoa(int(g.Count))
		}
		return strconv.FormatFloat(g.Count, 'g', -1, 64)
	}
	return ""
}

// primaryWorkload returns the execution/resources to run: the top-level spec, or
// (for multi-role jobs, which Run.ai models as a single workload) the largest
// task.
func primaryWorkload(job *splat.Job) (*splat.Execution, *splat.Resources) {
	e := &job.Spec.Execution
	if e.Container != nil || e.Script != "" || e.Executable != "" {
		return e, &job.Spec.Resources
	}
	var best *splat.Task
	for i := range job.Spec.Tasks {
		t := &job.Spec.Tasks[i]
		if best == nil || cpuOf(t) > cpuOf(best) {
			best = t
		}
	}
	if best == nil {
		return nil, nil
	}
	return best.Execution, best.Resources
}

func cpuOf(t *splat.Task) int {
	if t.Resources != nil {
		return t.Resources.CPUsPerTask
	}
	return 0
}

// workloadCommand returns (image, command, arguments) for the execution.
func workloadCommand(e *splat.Execution) (image, command, arguments string) {
	switch {
	case e.Container != nil && e.Container.Image != "":
		image = e.Container.Image
		command = strings.Join(e.Container.Command, " ")
		arguments = strings.Join(e.Container.Args, " ")
	case e.Script != "":
		image = "ubuntu:22.04"
		command = "/bin/bash -c"
		arguments = e.Script
	case e.Executable != "":
		image = "ubuntu:22.04"
		command = e.Executable
		arguments = e.Arguments
	}
	return image, command, arguments
}

func envItems(e *splat.Execution) *runaitypes.EnvItems {
	src := e.Environment.Vars
	if e.Container != nil && len(e.Container.Environment.Vars) > 0 {
		src = e.Container.Environment.Vars
	}
	if len(src) == 0 {
		return nil
	}
	items := map[string]runaitypes.StringValue{}
	for k, v := range src {
		items[k] = runaitypes.StringValue{Value: v}
	}
	return &runaitypes.EnvItems{Items: items}
}

func str(v string) *runaitypes.StringValue { return &runaitypes.StringValue{Value: v} }

func namespaceFor(job *splat.Job) string {
	if ns := job.Metadata.Annotations["bammm.io/namespace"]; ns != "" {
		return ns
	}
	project := job.Spec.Schedule.Project
	if project == "" {
		project = job.Spec.Schedule.Queue
	}
	if project == "" {
		project = "default"
	}
	return "runai-" + project
}
