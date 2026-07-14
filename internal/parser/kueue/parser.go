// Package kueue parses Kueue-admitted Kubernetes batch/v1 Jobs into SPLAT jobs.
// It is the inverse of internal/emitter/kueue.
package kueue

import (
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("kueue", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

const queueNameLabel = "kueue.x-k8s.io/queue-name"

// Parse converts a Kueue-admitted batch/v1 Job manifest into a SPLAT job.
// The input may be a multi-document YAML stream containing the Job plus a
// supporting ConfigMap (from which an embedded HPC script is recovered).
func Parse(data []byte) (*splat.Job, error) {
	docs := splitDocs(data)

	var k8sJob *batchv1.Job
	configMaps := map[string]map[string]string{} // name -> data

	for _, doc := range docs {
		kind := detectKind(doc)
		switch kind {
		case "Job":
			var j batchv1.Job
			if err := yaml.Unmarshal(doc, &j); err != nil {
				return nil, fmt.Errorf("kueue: unmarshal Job: %w", err)
			}
			k8sJob = &j
		case "ConfigMap":
			var cm corev1.ConfigMap
			if err := yaml.Unmarshal(doc, &cm); err != nil {
				return nil, fmt.Errorf("kueue: unmarshal ConfigMap: %w", err)
			}
			configMaps[cm.Name] = cm.Data
		}
	}

	if k8sJob == nil {
		return nil, fmt.Errorf("kueue: no batch/v1 Job document found")
	}

	job := &splat.Job{
		APIVersion: splat.APIVersion,
		Kind:       splat.Kind,
	}
	job.Metadata.Name = k8sJob.Name
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "kueue"}
	if k8sJob.Namespace != "" && k8sJob.Namespace != "default" {
		job.Metadata.Annotations["bammm.io/namespace"] = k8sJob.Namespace
	}

	applyLabels(job, k8sJob.Labels)
	applySpec(job, &k8sJob.Spec)
	applyPodSpec(job, &k8sJob.Spec.Template.Spec, configMaps)

	return job, nil
}

func applyLabels(job *splat.Job, labels map[string]string) {
	for k, v := range labels {
		switch k {
		case queueNameLabel:
			job.Spec.Schedule.Queue = v
		case "bammm.io/source-format":
			// origin marker; do not echo into user labels
		default:
			if job.Metadata.Labels == nil {
				job.Metadata.Labels = map[string]string{}
			}
			job.Metadata.Labels[k] = v
		}
	}
}

func applySpec(job *splat.Job, spec *batchv1.JobSpec) {
	if spec.Parallelism != nil && *spec.Parallelism > 0 {
		job.Spec.Resources.Tasks = int(*spec.Parallelism)
	}
	if spec.ActiveDeadlineSeconds != nil && *spec.ActiveDeadlineSeconds > 0 {
		d := time.Duration(*spec.ActiveDeadlineSeconds) * time.Second
		job.Spec.Schedule.Walltime = splat.DurationOf(d)
	}
	if spec.BackoffLimit != nil && *spec.BackoffLimit > 0 {
		job.Spec.Lifecycle.MaxRetries = int(*spec.BackoffLimit)
	}
	if spec.Completions != nil && *spec.Completions > 0 && job.Spec.Resources.Tasks == 0 {
		job.Spec.Resources.Tasks = int(*spec.Completions)
	}
}

func applyPodSpec(job *splat.Job, pod *corev1.PodSpec, configMaps map[string]map[string]string) {
	if len(pod.NodeSelector) > 0 {
		job.Spec.Placement.NodeSelector = pod.NodeSelector
	}
	if len(pod.Containers) == 0 {
		return
	}
	c := pod.Containers[0]

	applyResources(job, &c)

	// If the container runs a script mounted from a ConfigMap, recover it as an
	// HPC script (the inverse of the emitter's HPC→container path). Otherwise
	// keep it as a container execution.
	if script := scriptFromConfigMap(&c, pod, configMaps); script != "" {
		job.Spec.Execution.Script = script
	} else {
		job.Spec.Execution.Container = &splat.ContainerExecution{
			Image:   c.Image,
			Command: c.Command,
			Args:    c.Args,
		}
	}

	if wd := c.WorkingDir; wd != "" {
		job.Spec.Execution.WorkingDir = wd
	}
	if env := envMap(c.Env); len(env) > 0 {
		job.Spec.Execution.Environment.Vars = env
	}
}

func applyResources(job *splat.Job, c *corev1.Container) {
	r := &job.Spec.Resources
	reqs := c.Resources.Requests
	if reqs == nil {
		reqs = c.Resources.Limits
	}
	if reqs == nil {
		return
	}
	if cpu, ok := reqs[corev1.ResourceCPU]; ok {
		r.CPUsPerTask = int(cpu.Value())
	}
	if mem, ok := reqs[corev1.ResourceMemory]; ok {
		r.MemoryPerTask = splat.QuantityOf(mem.Value())
	}
	if gpu, ok := reqs["nvidia.com/gpu"]; ok {
		r.GPU = &splat.GPURequest{Count: float64(gpu.Value())}
	}
}

// scriptFromConfigMap returns the embedded script if the container executes one
// mounted from a ConfigMap volume.
func scriptFromConfigMap(c *corev1.Container, pod *corev1.PodSpec, configMaps map[string]map[string]string) string {
	if len(configMaps) == 0 {
		return ""
	}
	// Map mount name -> ConfigMap name for this pod.
	cmVolumes := map[string]string{}
	for _, v := range pod.Volumes {
		if v.ConfigMap != nil {
			cmVolumes[v.Name] = v.ConfigMap.Name
		}
	}
	for _, m := range c.VolumeMounts {
		cmName, ok := cmVolumes[m.Name]
		if !ok {
			continue
		}
		data, ok := configMaps[cmName]
		if !ok {
			continue
		}
		// Prefer the conventional job.sh key, else the sole data entry.
		if s, ok := data["job.sh"]; ok {
			return s
		}
		if len(data) == 1 {
			for _, v := range data {
				return v
			}
		}
	}
	return ""
}

func envMap(env []corev1.EnvVar) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, e := range env {
		if e.ValueFrom != nil {
			continue // field/secret refs have no plain-string equivalent
		}
		out[e.Name] = e.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitDocs splits a YAML stream into individual documents on "---" separators,
// dropping empty/comment-only fragments.
func splitDocs(data []byte) [][]byte {
	var docs [][]byte
	for _, part := range strings.Split(string(data), "\n---") {
		part = strings.TrimLeft(part, "-\n")
		if strings.TrimSpace(stripComments(part)) == "" {
			continue
		}
		docs = append(docs, []byte(part))
	}
	return docs
}

// detectKind returns the value of the top-level "kind:" field in a document.
func detectKind(doc []byte) string {
	for _, line := range strings.Split(string(doc), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "kind:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:"))
		}
	}
	return ""
}

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
