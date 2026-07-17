// Package volcano emits Volcano vcjob manifests from SPLAT jobs.
// It is the inverse of internal/parser/volcano.
package volcano

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
	volcanotypes "github.com/InsightSoftmax/BAMMM/internal/volcano"
)

func init() {
	emitter.Register("volcano", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// Emit converts a SPLAT job into a Volcano vcjob. Multi-role jobs (spec.tasks)
// become vcjob tasks; single-role jobs become one task.
func Emit(job *splat.Job) ([]byte, error) {
	name := job.Metadata.Name
	if name == "" {
		name = "bammm-job"
	}
	ext := volcanoExt(job)

	vc := volcanotypes.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: volcanotypes.APIVersion, Kind: volcanotypes.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespaceFor(job),
			Labels:    job.Metadata.Labels,
		},
		Spec: volcanotypes.JobSpec{
			SchedulerName:     schedulerName(ext),
			Queue:             job.Spec.Schedule.Queue,
			PriorityClassName: job.Spec.Schedule.PriorityClass,
			MaxRetry:          int32(job.Spec.Lifecycle.MaxRetries),
		},
	}

	if ttl := job.Spec.Lifecycle.TTLAfterFinished; ttl != nil {
		secs := int32(ttl.Duration().Seconds())
		vc.Spec.TTLSecondsAfterFinished = &secs
	}
	if plugins := pluginsExt(ext); len(plugins) > 0 {
		vc.Spec.Plugins = plugins
	}
	if policies := policiesExt(ext); len(policies) > 0 {
		vc.Spec.Policies = policies
	}

	tasks := job.Spec.Tasks
	if len(tasks) == 0 {
		tasks = []splat.Task{{
			Name:      "main",
			Replicas:  1,
			Resources: &job.Spec.Resources,
			Execution: &job.Spec.Execution,
		}}
	}

	total := 0
	for i := range tasks {
		vt, replicas, err := buildTask(job, tasks[i])
		if err != nil {
			return nil, err
		}
		total += replicas
		vc.Spec.Tasks = append(vc.Spec.Tasks, vt)
	}

	// minAvailable: gang requirement if set, else every replica (all-or-nothing).
	if g := job.Spec.Gang; g != nil && g.MinAvailable > 0 {
		vc.Spec.MinAvailable = int32(g.MinAvailable)
	} else {
		vc.Spec.MinAvailable = int32(total)
	}

	out, err := k8senc.MarshalClean(vc)
	if err != nil {
		return nil, fmt.Errorf("volcano: marshal: %w", err)
	}
	return out, nil
}

func buildTask(job *splat.Job, task splat.Task) (volcanotypes.Task, int, error) {
	replicas := task.Replicas
	if replicas == 0 {
		replicas = 1
	}
	pod, err := podSpec(job, task)
	if err != nil {
		return volcanotypes.Task{}, 0, err
	}
	return volcanotypes.Task{
		Name:     taskName(task),
		Replicas: int32(replicas),
		Template: corev1.PodTemplateSpec{Spec: *pod},
	}, replicas, nil
}

func podSpec(job *splat.Job, task splat.Task) (*corev1.PodSpec, error) {
	exec := task.Execution
	if exec == nil {
		return nil, fmt.Errorf("volcano: task %q has no execution", task.Name)
	}
	c := corev1.Container{Name: taskName(task)}

	switch {
	case exec.Container != nil && exec.Container.Image != "":
		c.Image = exec.Container.Image
		c.Command = exec.Container.Command
		c.Args = exec.Container.Args
		c.Env = k8senc.EnvVars(exec.Container.Environment)
		if c.Env == nil {
			c.Env = k8senc.EnvVars(exec.Environment)
		}
	case exec.Script != "":
		c.Command = []string{"/bin/bash", "-c"}
		c.Args = []string{exec.Script}
		c.Env = k8senc.EnvVars(exec.Environment)
	default:
		return nil, fmt.Errorf("volcano: task %q has no container image or script", task.Name)
	}

	if req := k8senc.ResourceRequirements(task.Resources); req != nil {
		c.Resources = *req
	}

	pod := &corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever}
	k8senc.AttachVolumes(pod, &c, job.Spec.Volumes)
	pod.Containers = []corev1.Container{c}

	if task.Placement != nil && len(task.Placement.Tolerations) > 0 {
		var tol []corev1.Toleration
		if err := k8senc.ConvertVia(task.Placement.Tolerations, &tol); err == nil {
			pod.Tolerations = tol
		}
	}
	return pod, nil
}

func taskName(task splat.Task) string {
	if task.Name != "" {
		return task.Name
	}
	return "main"
}

func namespaceFor(job *splat.Job) string {
	if ns := job.Metadata.Annotations["bammm.io/namespace"]; ns != "" {
		return ns
	}
	return "default"
}

func schedulerName(ext map[string]interface{}) string {
	if s, ok := ext["scheduler_name"].(string); ok && s != "" {
		return s
	}
	return "volcano"
}

// ── Extension helpers ──────────────────────────────────────────────────────

func volcanoExt(job *splat.Job) map[string]interface{} {
	if job.Spec.Extensions.Volcano != nil {
		return job.Spec.Extensions.Volcano
	}
	return map[string]interface{}{}
}

func pluginsExt(ext map[string]interface{}) map[string][]string {
	v, ok := ext["plugins"]
	if !ok {
		return nil
	}
	var out map[string][]string
	if err := k8senc.ConvertVia(v, &out); err != nil {
		return nil
	}
	return out
}

func policiesExt(ext map[string]interface{}) []volcanotypes.Policy {
	v, ok := ext["policies"]
	if !ok {
		return nil
	}
	var out []volcanotypes.Policy
	if err := k8senc.ConvertVia(v, &out); err != nil {
		return nil
	}
	return out
}
