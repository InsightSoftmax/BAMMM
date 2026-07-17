// Package kueue emits Kueue-admitted Kubernetes batch/v1 Jobs from SPLAT jobs.
//
// Kueue does not define its own job type; it queues existing Kubernetes Job
// objects tagged with the `kueue.x-k8s.io/queue-name` label. This emitter
// therefore produces:
//
//   - a batch/v1 Job carrying that label,
//   - a ConfigMap holding the HPC script when there is no container image, and
//   - a reference LocalQueue definition (commented as a required manual step).
package kueue

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("kueue", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

const (
	queueNameLabel = "kueue.x-k8s.io/queue-name"
	scriptMountDir = "/bammm"
	scriptFileName = "job.sh"
	defaultImage   = "ubuntu:22.04" // placeholder when the source has no container
)

// Emit converts a SPLAT job into a Kueue-admitted batch/v1 Job (plus supporting
// objects), rendered as a multi-document YAML stream.
func Emit(job *splat.Job) ([]byte, error) {
	name := job.Metadata.Name
	if name == "" {
		name = "bammm-job"
	}
	namespace := namespaceFor(job)

	var docs [][]byte

	// ── ConfigMap for an embedded HPC script (HPC → container path) ──────────
	needScriptCM := job.Spec.Execution.Container == nil && job.Spec.Execution.Script != ""
	if needScriptCM {
		cm := scriptConfigMap(name, namespace, job)
		out, err := marshalObject(cm)
		if err != nil {
			return nil, err
		}
		docs = append(docs, out)
	}

	// ── The Job itself ───────────────────────────────────────────────────────
	k8sJob, err := buildJob(name, namespace, job, needScriptCM)
	if err != nil {
		return nil, err
	}
	out, err := marshalObject(k8sJob)
	if err != nil {
		return nil, err
	}
	docs = append(docs, out)

	// ── Reference LocalQueue (informational) ─────────────────────────────────
	if q := queueName(job); q != "" {
		docs = append(docs, localQueueRef(q, namespace))
	}

	return bytes(docs), nil
}

func buildJob(name, namespace string, job *splat.Job, scriptCM bool) (*batchv1.Job, error) {
	labels := map[string]string{}
	for k, v := range job.Metadata.Labels {
		labels[k] = v
	}
	if sf := job.Metadata.Annotations["bammm.io/source-format"]; sf != "" {
		labels["bammm.io/source-format"] = sf
	}
	if q := queueName(job); q != "" {
		labels[queueNameLabel] = q
	}

	parallelism := replicaCount(job)
	container, volumes, err := buildContainer(name, job, scriptCM)
	if err != nil {
		return nil, err
	}

	spec := batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{container},
				Volumes:       volumes,
			},
		},
	}
	if parallelism > 1 {
		p := int32(parallelism)
		mode := batchv1.IndexedCompletion
		spec.Parallelism = &p
		spec.Completions = &p
		spec.CompletionMode = &mode
	}
	if secs := activeDeadline(job); secs > 0 {
		spec.ActiveDeadlineSeconds = &secs
	}
	// Kueue manages requeueing at the workload level; default a job to no
	// in-pod retries unless the source specified some.
	backoff := int32(job.Spec.Lifecycle.MaxRetries)
	spec.BackoffLimit = &backoff

	if ns := job.Spec.Placement.NodeSelector; len(ns) > 0 {
		spec.Template.Spec.NodeSelector = ns
	}

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: spec,
	}, nil
}

// buildContainer produces the workload container plus any volumes it needs.
func buildContainer(jobName string, job *splat.Job, scriptCM bool) (corev1.Container, []corev1.Volume, error) {
	e := job.Spec.Execution
	c := corev1.Container{Name: "main"}
	var volumes []corev1.Volume

	switch {
	case e.Container != nil && e.Container.Image != "":
		c.Image = e.Container.Image
		c.Command = e.Container.Command
		c.Args = e.Container.Args
		c.Env = k8senc.EnvVars(e.Container.Environment)
	case scriptCM:
		c.Image = defaultImage
		c.Command = []string{"/bin/bash", scriptMountDir + "/" + scriptFileName}
		c.Env = k8senc.EnvVars(e.Environment)
		c.VolumeMounts = []corev1.VolumeMount{{
			Name:      "bammm-script",
			MountPath: scriptMountDir,
			ReadOnly:  true,
		}}
		volumes = append(volumes, corev1.Volume{
			Name: "bammm-script",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: jobName + "-script"},
				},
			},
		})
	case e.Executable != "":
		c.Image = defaultImage
		c.Command = append([]string{e.Executable}, splitArgs(e.Arguments)...)
		c.Env = k8senc.EnvVars(e.Environment)
	default:
		return corev1.Container{}, nil, fmt.Errorf("kueue: job has no container image, script, or executable to run")
	}

	if e.WorkingDir != "" {
		c.WorkingDir = e.WorkingDir
	}
	if req := k8senc.ResourceRequirements(&job.Spec.Resources); req != nil {
		c.Resources = *req
	}
	return c, volumes, nil
}

func scriptConfigMap(name, namespace string, job *splat.Job) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-script",
			Namespace: namespace,
			Labels:    map[string]string{"bammm.io/job": name},
		},
		Data: map[string]string{scriptFileName: job.Spec.Execution.Script},
	}
}

// replicaCount picks the pod parallelism: gang minimum, else task count, else 1.
func replicaCount(job *splat.Job) int {
	if g := job.Spec.Gang; g != nil && g.MinAvailable > 0 {
		return g.MinAvailable
	}
	if job.Spec.Resources.Tasks > 0 {
		return job.Spec.Resources.Tasks
	}
	if job.Spec.Resources.Nodes > 0 {
		return job.Spec.Resources.Nodes
	}
	return 1
}

func activeDeadline(job *splat.Job) int64 {
	if w := job.Spec.Schedule.Walltime; w != nil {
		return int64(w.Duration().Seconds())
	}
	return 0
}

func queueName(job *splat.Job) string {
	if job.Spec.Schedule.Queue != "" {
		return job.Spec.Schedule.Queue
	}
	return job.Spec.Schedule.Partition
}

func namespaceFor(job *splat.Job) string {
	if ns := job.Metadata.Labels["bammm.io/namespace"]; ns != "" {
		return ns
	}
	if ns := job.Metadata.Annotations["bammm.io/namespace"]; ns != "" {
		return ns
	}
	return "default"
}

func localQueueRef(queue, namespace string) []byte {
	return []byte(fmt.Sprintf(`# Reference LocalQueue — must exist before submitting the Job above.
# Create the backing ClusterQueue with appropriate quota, then:
#   kubectl apply -f - <<'EOF'
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: %s
  namespace: %s
spec:
  clusterQueue: %s-cluster-queue # MANUAL: must exist and have quota
`, queue, namespace, queue))
}

func marshalObject(obj interface{}) ([]byte, error) {
	out, err := k8senc.MarshalClean(obj)
	if err != nil {
		return nil, fmt.Errorf("kueue: marshal: %w", err)
	}
	return out, nil
}

func bytes(docs [][]byte) []byte {
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.Write(d)
		if !strings.HasSuffix(string(d), "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func splitArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}
