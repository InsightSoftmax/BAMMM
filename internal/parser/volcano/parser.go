// Package volcano parses Volcano vcjob manifests into SPLAT jobs.
// It is the inverse of internal/emitter/volcano.
package volcano

import (
	"bytes"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
	volcanotypes "github.com/InsightSoftmax/BAMMM/internal/volcano"
)

// findVCJobDoc returns the batch.volcano.sh Job document from a (possibly
// multi-document) YAML stream.
func findVCJobDoc(data []byte) ([]byte, error) {
	docs := k8senc.SplitYAMLDocs(data)
	if len(docs) == 0 {
		return nil, fmt.Errorf("volcano: empty input")
	}
	for _, d := range docs {
		if k8senc.DocumentKind(d) == volcanotypes.Kind && bytes.Contains(d, []byte("batch.volcano.sh")) {
			return d, nil
		}
	}
	if len(docs) == 1 {
		return docs[0], nil // single doc — let unmarshal/kind checks report
	}
	return nil, fmt.Errorf("volcano: no batch.volcano.sh Job document found")
}

func init() {
	parser.Register("volcano", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts a Volcano vcjob manifest into a SPLAT job. Each vcjob task
// becomes a SPLAT task; minAvailable becomes spec.gang.
func Parse(data []byte) (*splat.Job, error) {
	// vcjobs commonly ship in multi-document bundles (Namespace + Queue + the
	// Job). Find the batch.volcano.sh Job document.
	doc, err := findVCJobDoc(data)
	if err != nil {
		return nil, err
	}
	var vc volcanotypes.Job
	if err := yaml.Unmarshal(doc, &vc); err != nil {
		return nil, fmt.Errorf("volcano: unmarshal: %w", err)
	}
	if len(vc.Spec.Tasks) == 0 {
		return nil, fmt.Errorf("volcano: vcjob has no tasks")
	}

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Name = vc.Name
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "volcano"}
	if vc.Namespace != "" && vc.Namespace != "default" {
		job.Metadata.Annotations["bammm.io/namespace"] = vc.Namespace
	}
	if len(vc.Labels) > 0 {
		job.Metadata.Labels = vc.Labels
	}

	job.Spec.Schedule.Queue = vc.Spec.Queue
	job.Spec.Schedule.PriorityClass = vc.Spec.PriorityClassName

	if vc.Spec.MinAvailable > 0 {
		job.Spec.Gang = &splat.Gang{MinAvailable: int(vc.Spec.MinAvailable), Style: splat.GangStyleHard}
	}
	if vc.Spec.MaxRetry > 0 {
		job.Spec.Lifecycle.MaxRetries = int(vc.Spec.MaxRetry)
	}
	if vc.Spec.TTLSecondsAfterFinished != nil {
		d := time.Duration(*vc.Spec.TTLSecondsAfterFinished) * time.Second
		job.Spec.Lifecycle.TTLAfterFinished = splat.DurationOf(d)
	}

	volumes := map[string]splat.Volume{}
	for i := range vc.Spec.Tasks {
		job.Spec.Tasks = append(job.Spec.Tasks, taskFromVolcano(&vc.Spec.Tasks[i], volumes))
	}
	job.Spec.Volumes = collectVolumes(volumes)

	applyExtensions(job, &vc.Spec)
	return job, nil
}

func taskFromVolcano(vt *volcanotypes.Task, volumes map[string]splat.Volume) splat.Task {
	task := splat.Task{Name: vt.Name, Replicas: int(vt.Replicas)}
	if task.Replicas == 0 {
		task.Replicas = 1
	}
	pod := vt.Template.Spec
	if len(pod.Containers) == 0 {
		return task
	}
	c := pod.Containers[0]

	task.Resources = k8senc.ResourcesFromContainer(&c)
	exec := &splat.Execution{
		Container: &splat.ContainerExecution{
			Image:   c.Image,
			Command: c.Command,
			Args:    c.Args,
		},
	}
	if env := k8senc.EnvMap(c.Env); len(env) > 0 {
		exec.Environment.Vars = env
	}
	task.Execution = exec

	if len(pod.Tolerations) > 0 {
		task.Placement = &splat.Placement{Tolerations: tolerations(pod.Tolerations)}
	}
	recordVolumes(&c, pod.Volumes, volumes)
	return task
}

// recordVolumes maps a container's volume mounts (paired with the pod's volume
// sources) into shared SPLAT volumes, keyed by name so identical per-task
// volumes are deduplicated at the job level.
func recordVolumes(c *corev1.Container, podVolumes []corev1.Volume, out map[string]splat.Volume) {
	sources := map[string]corev1.Volume{}
	for _, v := range podVolumes {
		sources[v.Name] = v
	}
	for _, m := range c.VolumeMounts {
		if _, seen := out[m.Name]; seen {
			continue
		}
		v := splat.Volume{Name: m.Name, MountPath: m.MountPath, ReadOnly: m.ReadOnly}
		if src, ok := sources[m.Name]; ok {
			switch {
			case src.PersistentVolumeClaim != nil:
				v.PVC = src.PersistentVolumeClaim.ClaimName
			case src.ConfigMap != nil:
				v.ConfigMap = src.ConfigMap.Name
			case src.Secret != nil:
				v.Secret = src.Secret.SecretName
			case src.HostPath != nil:
				v.HostPath = src.HostPath.Path
			case src.EmptyDir != nil:
				v.EmptyDir = true
			}
		}
		out[m.Name] = v
	}
}

func collectVolumes(m map[string]splat.Volume) []splat.Volume {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	// Stable order by name.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	out := make([]splat.Volume, 0, len(names))
	for _, n := range names {
		out = append(out, m[n])
	}
	return out
}

func applyExtensions(job *splat.Job, spec *volcanotypes.JobSpec) {
	ext := map[string]interface{}{}
	if spec.SchedulerName != "" {
		ext["scheduler_name"] = spec.SchedulerName
	}
	if len(spec.Plugins) > 0 {
		ext["plugins"] = spec.Plugins
	}
	if len(spec.Policies) > 0 {
		ext["policies"] = spec.Policies
	}
	if len(ext) > 0 {
		job.Spec.Extensions.Volcano = ext
	}
}

func tolerations(ts []corev1.Toleration) []interface{} {
	out := make([]interface{}, 0, len(ts))
	for i := range ts {
		out = append(out, ts[i])
	}
	return out
}
