package kueue

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/InsightSoftmax/BAMMM/internal/jobset"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// emitJobSet renders a multi-role SPLAT job as a Kueue-admitted JobSet: one
// replicatedJob per task, each a batch/v1 Job template. The queue-name label on
// the JobSet drives Kueue admission.
func emitJobSet(name, namespace string, job *splat.Job) ([]byte, error) {
	js := &jobset.JobSet{
		TypeMeta: metav1.TypeMeta{APIVersion: jobset.APIVersion, Kind: jobset.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    admissionLabels(job),
		},
	}
	for _, task := range job.Spec.Tasks {
		rj, err := buildReplicatedJob(job, task)
		if err != nil {
			return nil, err
		}
		js.Spec.ReplicatedJobs = append(js.Spec.ReplicatedJobs, rj)
	}

	out, err := marshalObject(js)
	if err != nil {
		return nil, err
	}
	docs := [][]byte{out}
	if q := queueName(job); q != "" {
		docs = append(docs, localQueueRef(q, namespace))
	}
	return bytes(docs), nil
}

func buildReplicatedJob(job *splat.Job, task splat.Task) (jobset.ReplicatedJob, error) {
	container, err := taskContainer(task)
	if err != nil {
		return jobset.ReplicatedJob{}, err
	}

	pod := corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever}
	k8senc.AttachVolumes(&pod, &container, job.Spec.Volumes)
	pod.Containers = []corev1.Container{container}
	if task.Placement != nil {
		if len(task.Placement.NodeSelector) > 0 {
			pod.NodeSelector = task.Placement.NodeSelector
		}
		if len(task.Placement.Tolerations) > 0 {
			var tol []corev1.Toleration
			if err := k8senc.ConvertVia(task.Placement.Tolerations, &tol); err == nil {
				pod.Tolerations = tol
			}
		}
	}

	jobSpec := batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{Spec: pod},
	}
	if r := task.Replicas; r > 1 {
		n := int32(r)
		mode := batchv1.IndexedCompletion
		jobSpec.Parallelism = &n
		jobSpec.Completions = &n
		jobSpec.CompletionMode = &mode
	}
	backoff := int32(job.Spec.Lifecycle.MaxRetries)
	jobSpec.BackoffLimit = &backoff
	if secs := activeDeadline(job); secs > 0 {
		jobSpec.ActiveDeadlineSeconds = &secs
	}

	replicas := int32(1)
	return jobset.ReplicatedJob{
		Name:     taskName(task),
		Replicas: replicas,
		Template: batchv1.JobTemplateSpec{Spec: jobSpec},
	}, nil
}

// taskContainer builds the container for one SPLAT task.
func taskContainer(task splat.Task) (corev1.Container, error) {
	e := task.Execution
	if e == nil {
		return corev1.Container{}, fmt.Errorf("kueue: task %q has no execution", task.Name)
	}
	c := corev1.Container{Name: taskName(task)}

	switch {
	case e.Container != nil && e.Container.Image != "":
		c.Image = e.Container.Image
		c.Command = e.Container.Command
		c.Args = e.Container.Args
		c.Env = k8senc.EnvVars(e.Container.Environment)
		if c.Env == nil {
			c.Env = k8senc.EnvVars(e.Environment)
		}
	case e.Script != "":
		c.Image = defaultImage
		c.Command = []string{"/bin/bash", "-c"}
		c.Args = []string{e.Script}
		c.Env = k8senc.EnvVars(e.Environment)
	case e.Executable != "":
		c.Image = defaultImage
		c.Command = append([]string{e.Executable}, splitArgs(e.Arguments)...)
		c.Env = k8senc.EnvVars(e.Environment)
	default:
		return corev1.Container{}, fmt.Errorf("kueue: task %q has no container image, script, or executable", task.Name)
	}

	if e.WorkingDir != "" {
		c.WorkingDir = e.WorkingDir
	}
	if req := k8senc.ResourceRequirements(task.Resources); req != nil {
		c.Resources = *req
	}
	return c, nil
}

func taskName(task splat.Task) string {
	if task.Name != "" {
		return task.Name
	}
	return "main"
}
