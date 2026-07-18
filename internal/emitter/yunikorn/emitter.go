// Package yunikorn emits YuniKorn-scheduled Kubernetes workloads from SPLAT jobs.
//
// YuniKorn is a scheduler plugin, not a workload CRD: it schedules standard
// batch/v1 Jobs (and JobSets for multi-role) that carry its app-id / queue
// labels and set schedulerName: yunikorn. Gang scheduling is expressed with the
// yunikorn.apache.org/task-groups annotation.
package yunikorn

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/jobset"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("yunikorn", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

const (
	appIDLabel       = "yunikorn.apache.org/app-id"
	queueLabel       = "yunikorn.apache.org/queue"
	taskGroupsAnn    = "yunikorn.apache.org/task-groups"
	taskGroupNameAnn = "yunikorn.apache.org/task-group-name"
	schedulerName    = "yunikorn"
	defaultImage     = "ubuntu:22.04"
	taskGroupName    = "members"
)

// Emit converts a SPLAT job into a YuniKorn-scheduled batch/v1 Job (single role)
// or JobSet (multi-role).
func Emit(job *splat.Job) ([]byte, error) {
	name := job.Metadata.Name
	if name == "" {
		name = "bammm-job"
	}
	namespace := namespaceFor(job)

	if len(job.Spec.Tasks) > 0 {
		return emitJobSet(name, namespace, job)
	}

	container, err := containerFor("main", &job.Spec.Execution, &job.Spec.Resources)
	if err != nil {
		return nil, err
	}
	pod := podTemplate(container, job.Spec.Volumes, job.Spec.Placement)
	if g := job.Spec.Gang; g != nil && g.MinAvailable > 0 {
		groups := []map[string]interface{}{group(taskGroupName, g.MinAvailable, &job.Spec.Resources)}
		setGang(&pod, taskGroupName, groups)
	}
	spec := batchv1.JobSpec{Template: pod}
	if n := replicaCount(job); n > 1 {
		p := int32(n)
		mode := batchv1.IndexedCompletion
		spec.Parallelism, spec.Completions, spec.CompletionMode = &p, &p, &mode
	}
	backoff := int32(job.Spec.Lifecycle.MaxRetries)
	spec.BackoffLimit = &backoff

	k8sJob := &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labelsFor(name, job)},
		Spec:       spec,
	}
	return k8senc.MarshalClean(k8sJob)
}

func emitJobSet(name, namespace string, job *splat.Job) ([]byte, error) {
	js := &jobset.JobSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: jobset.APIVersion, Kind: jobset.Kind},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labelsFor(name, job)},
	}
	// Gang scheduling across roles: one task group per role, listed on every pod.
	gangOn := job.Spec.Gang != nil && job.Spec.Gang.MinAvailable > 0
	var groups []map[string]interface{}
	if gangOn {
		for _, t := range job.Spec.Tasks {
			mm := t.Replicas
			if mm < 1 {
				mm = 1
			}
			groups = append(groups, group(taskName(t), mm, t.Resources))
		}
	}

	for i := range job.Spec.Tasks {
		task := job.Spec.Tasks[i]
		container, err := containerFor(taskName(task), task.Execution, task.Resources)
		if err != nil {
			return nil, err
		}
		var place splat.Placement
		if task.Placement != nil {
			place = *task.Placement
		}
		pod := podTemplate(container, job.Spec.Volumes, place)
		if gangOn {
			setGang(&pod, taskName(task), groups)
		}
		replicas := int32(1)
		jobSpec := batchv1.JobSpec{Template: pod}
		if task.Replicas > 1 {
			p := int32(task.Replicas)
			mode := batchv1.IndexedCompletion
			jobSpec.Parallelism, jobSpec.Completions, jobSpec.CompletionMode = &p, &p, &mode
		}
		js.Spec.ReplicatedJobs = append(js.Spec.ReplicatedJobs, jobset.ReplicatedJob{
			Name:     taskName(task),
			Replicas: replicas,
			Template: batchv1.JobTemplateSpec{Spec: jobSpec},
		})
	}
	return k8senc.MarshalClean(js)
}

// podTemplate builds a pod template scheduled by YuniKorn.
func podTemplate(c corev1.Container, volumes []splat.Volume, place splat.Placement) corev1.PodTemplateSpec {
	pod := corev1.PodSpec{
		SchedulerName: schedulerName,
		RestartPolicy: corev1.RestartPolicyNever,
	}
	k8senc.AttachVolumes(&pod, &c, volumes)
	pod.Containers = []corev1.Container{c}
	if len(place.NodeSelector) > 0 {
		pod.NodeSelector = place.NodeSelector
	}
	if len(place.Tolerations) > 0 {
		var tol []corev1.Toleration
		if err := k8senc.ConvertVia(place.Tolerations, &tol); err == nil {
			pod.Tolerations = tol
		}
	}
	return corev1.PodTemplateSpec{Spec: pod}
}

func containerFor(name string, e *splat.Execution, r *splat.Resources) (corev1.Container, error) {
	if e == nil {
		return corev1.Container{}, fmt.Errorf("yunikorn: %q has no execution", name)
	}
	c := corev1.Container{Name: name}
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
		c.Command = append([]string{e.Executable}, strings.Fields(e.Arguments)...)
		c.Env = k8senc.EnvVars(e.Environment)
	default:
		return corev1.Container{}, fmt.Errorf("yunikorn: %q has no container image, script, or executable", name)
	}
	if req := k8senc.ResourceRequirements(r); req != nil {
		c.Resources = *req
	}
	return c, nil
}

// group builds one YuniKorn task group (a gang member set).
func group(name string, minMember int, r *splat.Resources) map[string]interface{} {
	g := map[string]interface{}{"name": name, "minMember": minMember}
	if min := minResource(r); len(min) > 0 {
		g["minResource"] = min
	}
	return g
}

// setGang stamps the task-group-name and task-groups annotations onto a pod
// template. YuniKorn requires the full task-groups list on every pod.
func setGang(pod *corev1.PodTemplateSpec, groupName string, groups []map[string]interface{}) {
	b, err := json.Marshal(groups)
	if err != nil {
		return
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[taskGroupNameAnn] = groupName
	pod.Annotations[taskGroupsAnn] = string(b)
}

func minResource(r *splat.Resources) map[string]string {
	m := map[string]string{}
	if r == nil {
		return m
	}
	if r.CPUsPerTask > 0 {
		m["cpu"] = strconv.Itoa(r.CPUsPerTask)
	}
	if r.MemoryPerTask != nil {
		m["memory"] = r.MemoryPerTask.String()
	}
	if r.GPU != nil && r.GPU.Count > 0 {
		m[k8senc.GPUResourceName] = strconv.Itoa(int(r.GPU.Count + 0.5))
	}
	return m
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func labelsFor(name string, job *splat.Job) map[string]string {
	labels := map[string]string{}
	for k, v := range job.Metadata.Labels {
		labels[k] = v
	}
	if sf := job.Metadata.Annotations["bammm.io/source-format"]; sf != "" {
		labels["bammm.io/source-format"] = sf
	}
	labels[appIDLabel] = name
	labels[queueLabel] = queueValue(job)
	return labels
}

// queueValue maps a SPLAT queue to YuniKorn's hierarchical form (root.<queue>).
func queueValue(job *splat.Job) string {
	q := job.Spec.Schedule.Queue
	if q == "" {
		q = job.Spec.Schedule.Partition
	}
	if q == "" {
		return "root.default"
	}
	if strings.HasPrefix(q, "root.") {
		return q
	}
	return "root." + q
}

func replicaCount(job *splat.Job) int {
	if g := job.Spec.Gang; g != nil && g.MinAvailable > 0 {
		return g.MinAvailable
	}
	if job.Spec.Resources.Tasks > 0 {
		return job.Spec.Resources.Tasks
	}
	return 1
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
