// Package volcano parses Volcano vcjob manifests into SPLAT jobs.
// It is the inverse of internal/emitter/volcano.
package volcano

import (
	"bytes"
	"fmt"
	"time"

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
	job.Spec.Volumes = k8senc.SortVolumes(volumes)

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
		task.Placement = &splat.Placement{Tolerations: k8senc.Tolerations(pod.Tolerations)}
	}
	k8senc.VolumesFromPod(&c, &pod, volumes)
	return task
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
