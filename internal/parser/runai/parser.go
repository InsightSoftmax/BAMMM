// Package runai parses Run.ai (NVIDIA) TrainingWorkload manifests into SPLAT
// jobs. It is the inverse of internal/emitter/runai.
package runai

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	runaitypes "github.com/InsightSoftmax/BAMMM/internal/runai"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("runai", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts a Run.ai TrainingWorkload manifest into a SPLAT job.
func Parse(data []byte) (*splat.Job, error) {
	var wl *runaitypes.Workload
	for _, doc := range k8senc.SplitYAMLDocs(data) {
		if k8senc.DocumentKind(doc) == runaitypes.Kind {
			var w runaitypes.Workload
			if err := yaml.Unmarshal(doc, &w); err != nil {
				return nil, fmt.Errorf("runai: unmarshal: %w", err)
			}
			wl = &w
			break
		}
	}
	if wl == nil {
		return nil, fmt.Errorf("runai: no %s document found", runaitypes.Kind)
	}

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Name = wl.Name
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "runai"}
	if len(wl.Labels) > 0 {
		job.Metadata.Labels = wl.Labels
	}
	// Run.ai projects live in a runai-<project> namespace.
	if p := strings.TrimPrefix(wl.Namespace, "runai-"); p != "" && p != wl.Namespace {
		job.Spec.Schedule.Project = p
	}

	applySpec(job, &wl.Spec)
	return job, nil
}

func applySpec(job *splat.Job, spec *runaitypes.Spec) {
	r := &job.Spec.Resources
	if spec.CPU != nil {
		r.CPUsPerTask = parseCPU(spec.CPU.Value)
	}
	if spec.Memory != nil {
		if q, err := parseQuantity(spec.Memory.Value); err == nil {
			r.MemoryPerTask = q
		}
	}
	if spec.GPU != nil {
		r.GPU = parseGPU(spec.GPU.Value)
	}
	if spec.GPUMemory != nil && r.GPU != nil {
		if q, err := parseQuantity(spec.GPUMemory.Value); err == nil {
			r.GPU.Memory = q
		}
	}
	if spec.Parallelism != nil && spec.Parallelism.Value > 1 {
		r.Tasks = spec.Parallelism.Value
	} else if spec.Completions != nil && spec.Completions.Value > 1 {
		r.Tasks = spec.Completions.Value
	}

	applyExecution(job, spec)
}

func applyExecution(job *splat.Job, spec *runaitypes.Spec) {
	image := valueOf(spec.Image)
	command := valueOf(spec.Command)
	args := valueOf(spec.Arguments)

	// An inlined script (default image + /bin/bash -c) round-trips to a script.
	if image == "ubuntu:22.04" && command == "/bin/bash -c" {
		job.Spec.Execution.Script = args
	} else if image != "" {
		c := &splat.ContainerExecution{Image: image}
		if command != "" {
			c.Command = strings.Fields(command)
		}
		if args != "" {
			c.Args = strings.Fields(args)
		}
		job.Spec.Execution.Container = c
	}

	if spec.Environment != nil && len(spec.Environment.Items) > 0 {
		vars := map[string]string{}
		for k, v := range spec.Environment.Items {
			vars[k] = v.Value
		}
		job.Spec.Execution.Environment.Vars = vars
	}
}

func valueOf(v *runaitypes.StringValue) string {
	if v == nil {
		return ""
	}
	return v.Value
}

// parseGPU maps a Run.ai gpu value to a GPURequest. A fraction sets Fraction and
// rounds Count up to a whole GPU (the documented lossy translation for non-Run.ai
// targets); a whole number sets Count.
func parseGPU(s string) *splat.GPURequest {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return nil
	}
	if f == math.Trunc(f) {
		return &splat.GPURequest{Count: f}
	}
	return &splat.GPURequest{Fraction: f, Count: math.Ceil(f)}
}

// parseCPU parses a Kubernetes CPU quantity to whole cores (millicores rounded).
func parseCPU(s string) int {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "m") {
		if milli, err := strconv.Atoi(strings.TrimSuffix(s, "m")); err == nil {
			return int(math.Round(float64(milli) / 1000))
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(math.Round(f))
	}
	return 0
}

func parseQuantity(s string) (*splat.Quantity, error) {
	var q splat.Quantity
	if err := q.UnmarshalJSON([]byte(`"` + strings.TrimSpace(s) + `"`)); err != nil {
		return nil, err
	}
	return &q, nil
}
