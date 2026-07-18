// Package yunikorn parses YuniKorn-scheduled Kubernetes batch/v1 Jobs into SPLAT
// jobs. It is the inverse of internal/emitter/yunikorn.
package yunikorn

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/InsightSoftmax/BAMMM/internal/jobset"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("yunikorn", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

const (
	appIDLabel       = "yunikorn.apache.org/app-id"
	appIDLabelLegacy = "applicationId"
	queueLabel       = "yunikorn.apache.org/queue"
	queueLabelLegacy = "queue"
	taskGroupsAnn    = "yunikorn.apache.org/task-groups"
	defaultImage     = "ubuntu:22.04"
)

// Parse converts a YuniKorn-scheduled batch/v1 Job manifest into a SPLAT job.
func Parse(data []byte) (*splat.Job, error) {
	var k8sJob *batchv1.Job
	for _, doc := range k8senc.SplitYAMLDocs(data) {
		switch k8senc.DocumentKind(doc) {
		case jobset.Kind:
			var js jobset.JobSet
			if err := yaml.Unmarshal(doc, &js); err != nil {
				return nil, fmt.Errorf("yunikorn: unmarshal JobSet: %w", err)
			}
			return jobFromJobSet(&js), nil
		case "Job":
			if k8sJob != nil {
				continue
			}
			var j batchv1.Job
			if err := yaml.Unmarshal(doc, &j); err != nil {
				return nil, fmt.Errorf("yunikorn: unmarshal Job: %w", err)
			}
			k8sJob = &j
		}
	}
	if k8sJob == nil {
		return nil, fmt.Errorf("yunikorn: no batch/v1 Job document found")
	}

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Name = k8sJob.Name
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "yunikorn"}
	if k8sJob.Namespace != "" && k8sJob.Namespace != "default" {
		job.Metadata.Annotations["bammm.io/namespace"] = k8sJob.Namespace
	}

	applyLabels(job, k8sJob.Labels)
	applySpec(job, &k8sJob.Spec)
	applyPodSpec(job, &k8sJob.Spec.Template)
	return job, nil
}

// jobFromJobSet builds a multi-role SPLAT job from a YuniKorn-scheduled JobSet,
// recovering gang scheduling from the task-groups annotation the emitter stamps
// on every pod template.
func jobFromJobSet(js *jobset.JobSet) *splat.Job {
	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Name = js.Name
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "yunikorn"}
	if js.Namespace != "" && js.Namespace != "default" {
		job.Metadata.Annotations["bammm.io/namespace"] = js.Namespace
	}
	applyLabels(job, js.Labels)

	tasks, volumes, maxRetries := jobset.ToTasks(js)
	job.Spec.Tasks = tasks
	job.Spec.Volumes = volumes
	if maxRetries > 0 {
		job.Spec.Lifecycle.MaxRetries = maxRetries
	}
	for i := range js.Spec.ReplicatedJobs {
		applyGang(job, js.Spec.ReplicatedJobs[i].Template.Spec.Template.Annotations)
		if job.Spec.Gang != nil {
			break
		}
	}
	return job
}

func applyLabels(job *splat.Job, labels map[string]string) {
	for k, v := range labels {
		switch k {
		case queueLabel, queueLabelLegacy:
			job.Spec.Schedule.Queue = strings.TrimPrefix(v, "root.")
		case appIDLabel, appIDLabelLegacy, "bammm.io/source-format":
			// scheduling markers — not user metadata
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
	} else if spec.Completions != nil && *spec.Completions > 0 {
		job.Spec.Resources.Tasks = int(*spec.Completions)
	}
	if spec.BackoffLimit != nil && *spec.BackoffLimit > 0 {
		job.Spec.Lifecycle.MaxRetries = int(*spec.BackoffLimit)
	}
	if spec.ActiveDeadlineSeconds != nil && *spec.ActiveDeadlineSeconds > 0 {
		job.Spec.Schedule.Walltime = splat.DurationOf(time.Duration(*spec.ActiveDeadlineSeconds) * time.Second)
	}
}

func applyPodSpec(job *splat.Job, tmpl *corev1.PodTemplateSpec) {
	applyGang(job, tmpl.Annotations)

	pod := tmpl.Spec
	if len(pod.NodeSelector) > 0 {
		job.Spec.Placement.NodeSelector = pod.NodeSelector
	}
	if len(pod.Containers) == 0 {
		return
	}
	c := pod.Containers[0]
	if r := k8senc.ResourcesFromContainer(&c); r != nil {
		merged := job.Spec.Resources
		merged.CPUsPerTask = r.CPUsPerTask
		merged.MemoryPerTask = r.MemoryPerTask
		merged.GPU = r.GPU
		job.Spec.Resources = merged
	}

	// An inlined script (default image + /bin/bash -c) round-trips back to a
	// script execution; otherwise keep it as a container.
	if c.Image == defaultImage && len(c.Command) == 2 && c.Command[0] == "/bin/bash" && c.Command[1] == "-c" && len(c.Args) == 1 {
		job.Spec.Execution.Script = c.Args[0]
	} else {
		job.Spec.Execution.Container = &splat.ContainerExecution{Image: c.Image, Command: c.Command, Args: c.Args}
	}
	if env := k8senc.EnvMap(c.Env); len(env) > 0 {
		job.Spec.Execution.Environment.Vars = env
	}
}

// applyGang recovers gang scheduling from the task-groups annotation.
func applyGang(job *splat.Job, annotations map[string]string) {
	raw, ok := annotations[taskGroupsAnn]
	if !ok {
		return
	}
	var groups []struct {
		MinMember int `json:"minMember"`
	}
	if err := json.Unmarshal([]byte(raw), &groups); err != nil || len(groups) == 0 {
		return
	}
	minMember := 0
	for _, g := range groups {
		minMember += g.MinMember
	}
	if minMember > 0 {
		job.Spec.Gang = &splat.Gang{MinAvailable: minMember, Style: splat.GangStyleHard}
	}
}
