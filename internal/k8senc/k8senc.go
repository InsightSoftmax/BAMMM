// Package k8senc holds helpers shared by the Kubernetes-family parsers and
// emitters (Volcano, Kueue, Armada) for translating between SPLAT resource and
// environment types and their core/v1 equivalents.
package k8senc

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// GPUResourceName is the de-facto standard GPU resource key across the K8s ecosystem.
const GPUResourceName = "nvidia.com/gpu"

// ResourceRequirements builds a core/v1 requirements block from SPLAT resources,
// mirroring requests into limits (batch jobs are typically guaranteed QoS).
// Returns nil when there is nothing to request.
func ResourceRequirements(r *splat.Resources) *corev1.ResourceRequirements {
	if r == nil {
		return nil
	}
	reqs := corev1.ResourceList{}
	if r.CPUsPerTask > 0 {
		reqs[corev1.ResourceCPU] = *resource.NewQuantity(int64(r.CPUsPerTask), resource.DecimalSI)
	}
	if mem := memoryPerTask(r); mem != nil {
		reqs[corev1.ResourceMemory] = *mem
	}
	if r.GPU != nil && r.GPU.Count > 0 {
		reqs[GPUResourceName] = *resource.NewQuantity(int64(r.GPU.Count+0.5), resource.DecimalSI)
	}
	if len(reqs) == 0 {
		return nil
	}
	limits := corev1.ResourceList{}
	for k, v := range reqs {
		limits[k] = v
	}
	return &corev1.ResourceRequirements{Requests: reqs, Limits: limits}
}

// memoryPerTask returns the per-pod memory, deriving it from mem-per-cpu ×
// cpus-per-task when only the per-CPU figure is present.
func memoryPerTask(r *splat.Resources) *resource.Quantity {
	switch {
	case r.MemoryPerTask != nil:
		return resource.NewQuantity(r.MemoryPerTask.Bytes(), resource.BinarySI)
	case r.MemoryPerCPU != nil && r.CPUsPerTask > 0:
		return resource.NewQuantity(r.MemoryPerCPU.Bytes()*int64(r.CPUsPerTask), resource.BinarySI)
	case r.MemoryPerCPU != nil:
		return resource.NewQuantity(r.MemoryPerCPU.Bytes(), resource.BinarySI)
	default:
		return nil
	}
}

// ResourcesFromContainer extracts SPLAT resources from a container's requests
// (falling back to limits). Returns nil when neither is set.
func ResourcesFromContainer(c *corev1.Container) *splat.Resources {
	reqs := c.Resources.Requests
	if reqs == nil {
		reqs = c.Resources.Limits
	}
	if reqs == nil {
		return nil
	}
	r := &splat.Resources{}
	if cpu, ok := reqs[corev1.ResourceCPU]; ok {
		r.CPUsPerTask = int(cpu.Value())
	}
	if mem, ok := reqs[corev1.ResourceMemory]; ok {
		r.MemoryPerTask = splat.QuantityOf(mem.Value())
	}
	if gpu, ok := reqs[GPUResourceName]; ok {
		r.GPU = &splat.GPURequest{Count: float64(gpu.Value())}
	}
	return r
}

// EnvVars converts a SPLAT environment's plain vars into sorted core/v1 EnvVars.
func EnvVars(env splat.Environment) []corev1.EnvVar {
	if len(env.Vars) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env.Vars))
	for k := range env.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: env.Vars[k]})
	}
	return out
}

// EnvMap converts core/v1 EnvVars into a plain map, skipping valueFrom refs
// (field/secret references have no plain-string equivalent).
func EnvMap(env []corev1.EnvVar) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, e := range env {
		if e.ValueFrom != nil {
			continue
		}
		out[e.Name] = e.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ConvertVia re-materializes a loosely-typed value (e.g. map[string]interface{}
// from a YAML round-trip, or a concrete struct) into dst by marshaling through
// YAML.
func ConvertVia(src, dst interface{}) error {
	data, err := yaml.Marshal(src)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dst)
}

// MarshalClean marshals a Kubernetes object to YAML, stripping the empty-value
// noise that k8s.io/api types always emit — a null "creationTimestamp", an
// empty "status", and any "metadata" left empty afterwards. This keeps output
// readable and valid against strict CRD schemas (some trim ObjectMeta and
// forbid unknown fields, so a stray creationTimestamp fails validation).
func MarshalClean(obj interface{}) ([]byte, error) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return yaml.Marshal(pruneNoise(root))
}

func pruneNoise(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			t[k] = pruneNoise(val)
		}
		if ts, ok := t["creationTimestamp"]; ok && ts == nil {
			delete(t, "creationTimestamp")
		}
		if st, ok := t["status"]; ok && isEmptyMap(st) {
			delete(t, "status")
		}
		if md, ok := t["metadata"]; ok && isEmptyMap(md) {
			delete(t, "metadata")
		}
		return t
	case []interface{}:
		for i := range t {
			t[i] = pruneNoise(t[i])
		}
		return t
	default:
		return v
	}
}

func isEmptyMap(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	return ok && len(m) == 0
}
