package jobset

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// DefaultImage is the placeholder image the Kueue/YuniKorn JobSet emitters use
// for script and executable tasks that carry no source container. The parser
// uses it to recognize that such a container is really an inlined script.
const DefaultImage = "ubuntu:22.04"

// ToTasks converts a JobSet's replicatedJobs into SPLAT tasks, plus the shared
// job-level volumes and the maximum in-pod retry (BackoffLimit) observed. It is
// the inverse of the Kueue and YuniKorn JobSet emitters; scheduler-specific
// metadata (queue labels, gang annotations) stays with the caller.
func ToTasks(js *JobSet) (tasks []splat.Task, volumes []splat.Volume, maxRetries int) {
	seen := map[string]splat.Volume{}
	for i := range js.Spec.ReplicatedJobs {
		rj := &js.Spec.ReplicatedJobs[i]
		spec := &rj.Template.Spec
		if bl := spec.BackoffLimit; bl != nil && int(*bl) > maxRetries {
			maxRetries = int(*bl)
		}

		task := splat.Task{Name: rj.Name, Replicas: replicasOf(spec.Parallelism, spec.Completions)}
		pod := &spec.Template.Spec
		if len(pod.Containers) > 0 {
			c := pod.Containers[0]
			task.Resources = k8senc.ResourcesFromContainer(&c)
			task.Execution = executionOf(&c)
			k8senc.VolumesFromPod(&c, pod, seen)
		}
		task.Placement = placementOf(pod)
		tasks = append(tasks, task)
	}
	return tasks, k8senc.SortVolumes(seen), maxRetries
}

// replicasOf recovers a task's replica count from the wrapped Job's parallelism
// (the emitters set parallelism/completions only when replicas exceed 1).
func replicasOf(parallelism, completions *int32) int {
	switch {
	case parallelism != nil && *parallelism > 0:
		return int(*parallelism)
	case completions != nil && *completions > 0:
		return int(*completions)
	default:
		return 1
	}
}

// executionOf inverts the emitters' container: an inlined "/bin/bash -c <script>"
// on the placeholder image round-trips back to a script; anything else stays a
// container execution.
func executionOf(c *corev1.Container) *splat.Execution {
	e := &splat.Execution{}
	if c.Image == DefaultImage && len(c.Command) == 2 &&
		c.Command[0] == "/bin/bash" && c.Command[1] == "-c" && len(c.Args) == 1 {
		e.Script = c.Args[0]
	} else {
		e.Container = &splat.ContainerExecution{Image: c.Image, Command: c.Command, Args: c.Args}
	}
	if c.WorkingDir != "" {
		e.WorkingDir = c.WorkingDir
	}
	if env := k8senc.EnvMap(c.Env); len(env) > 0 {
		e.Environment.Vars = env
	}
	return e
}

// placementOf recovers node selectors and tolerations, returning nil when the
// pod pins neither.
func placementOf(pod *corev1.PodSpec) *splat.Placement {
	pl := &splat.Placement{}
	set := false
	if len(pod.NodeSelector) > 0 {
		pl.NodeSelector = pod.NodeSelector
		set = true
	}
	if len(pod.Tolerations) > 0 {
		pl.Tolerations = k8senc.Tolerations(pod.Tolerations)
		set = true
	}
	if !set {
		return nil
	}
	return pl
}
