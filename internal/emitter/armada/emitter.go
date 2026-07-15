// Package armada emits Armada JobSubmitRequest YAML from SPLAT jobs.
// It is the inverse of internal/parser/armada.
package armada

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	armadatypes "github.com/InsightSoftmax/BAMMM/internal/armada"
	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("armada", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// Emit converts a SPLAT job into an Armada JobSubmitRequest. Multi-role jobs
// (spec.tasks) become one Armada job per task, linked by gang annotations.
func Emit(job *splat.Job) ([]byte, error) {
	ext := armadaExt(job)
	jobSetID := stringExt(ext, "job_set_id")
	if jobSetID == "" {
		jobSetID = job.Metadata.Name
	}

	req := armadatypes.Request{
		Queue:    job.Spec.Schedule.Queue,
		JobSetID: jobSetID,
	}

	tasks := job.Spec.Tasks
	if len(tasks) == 0 {
		// Single-role job: synthesize one task from the top-level spec.
		tasks = []splat.Task{{
			Name:      "main",
			Replicas:  1,
			Resources: &job.Spec.Resources,
			Execution: &job.Spec.Execution,
		}}
	}

	for i := range tasks {
		aj, err := buildJob(job, ext, jobSetID, tasks, i)
		if err != nil {
			return nil, err
		}
		req.Jobs = append(req.Jobs, aj)
	}

	// externalJobUri rides on the first job.
	if uri := job.Metadata.Annotations["bammm.io/external-uri"]; uri != "" && len(req.Jobs) > 0 {
		req.Jobs[0].ExternalJobURI = uri
	}

	if ing := ingressExt(ext); len(ing) > 0 {
		req.Ingress = ing
	}
	if svc := servicesExt(ext); len(svc) > 0 {
		req.Services = svc
	}

	out, err := yaml.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("armada: marshal: %w", err)
	}
	return out, nil
}

func buildJob(job *splat.Job, ext map[string]interface{}, jobSetID string, tasks []splat.Task, i int) (armadatypes.Job, error) {
	task := tasks[i]
	namespace := stringExt(ext, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	aj := armadatypes.Job{
		ClientID:  clientID(jobSetID, task, i),
		Priority:  priority(job, ext),
		Namespace: namespace,
		Scheduler: stringExt(ext, "scheduler_name"),
		Labels:    jobLabels(job, task),
	}

	if ann := gangAnnotations(job, ext, tasks); len(ann) > 0 {
		aj.Annotations = ann
	}

	pod, err := podSpec(job, task)
	if err != nil {
		return armadatypes.Job{}, err
	}
	aj.PodSpec = pod
	return aj, nil
}

func podSpec(job *splat.Job, task splat.Task) (*corev1.PodSpec, error) {
	exec := task.Execution
	if exec == nil {
		return nil, fmt.Errorf("armada: task %q has no execution", task.Name)
	}
	c := corev1.Container{Name: containerName(task)}

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
		return nil, fmt.Errorf("armada: task %q has no container image or script", task.Name)
	}

	if req := k8senc.ResourceRequirements(task.Resources); req != nil {
		c.Resources = *req
	}

	pod := &corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{c},
	}
	if pc := job.Spec.Schedule.PriorityClass; pc != "" {
		pod.PriorityClassName = pc
	}
	if task.Placement != nil && len(task.Placement.Tolerations) > 0 {
		var tol []corev1.Toleration
		if err := k8senc.ConvertVia(task.Placement.Tolerations, &tol); err == nil {
			pod.Tolerations = tol
		}
	}
	return pod, nil
}

func jobLabels(job *splat.Job, task splat.Task) map[string]string {
	labels := map[string]string{}
	for k, v := range job.Metadata.Labels {
		labels[k] = v
	}
	if task.Name != "" && len(job.Spec.Tasks) > 0 {
		labels["component"] = task.Name
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

func gangAnnotations(job *splat.Job, ext map[string]interface{}, tasks []splat.Task) map[string]string {
	g := job.Spec.Gang
	if g == nil {
		return nil
	}
	gangID := stringExt(ext, "gang_id")
	if gangID == "" {
		gangID = job.Metadata.Name + "-gang"
	}
	minSize := g.MinAvailable
	if minSize == 0 {
		minSize = len(tasks)
	}
	return map[string]string{
		armadatypes.AnnGangID:          gangID,
		armadatypes.AnnGangCardinality: strconv.Itoa(len(tasks)),
		armadatypes.AnnGangMinJobSize:  strconv.Itoa(minSize),
	}
}

func clientID(jobSetID string, task splat.Task, i int) string {
	name := task.Name
	if name == "" {
		name = strconv.Itoa(i)
	}
	if jobSetID == "" {
		return name
	}
	return jobSetID + "-" + name
}

func containerName(task splat.Task) string {
	if task.Name != "" {
		return task.Name
	}
	return "main"
}

// priority returns the Armada priority, preferring the round-tripped raw value
// over the normalized SPLAT priority.
func priority(job *splat.Job, ext map[string]interface{}) float64 {
	if v, ok := ext["priority"]; ok {
		if f, ok := floatValue(v); ok {
			return f
		}
	}
	return float64(job.Spec.Schedule.Priority)
}

// ── Extension helpers ──────────────────────────────────────────────────────

func armadaExt(job *splat.Job) map[string]interface{} {
	if job.Spec.Extensions.Armada != nil {
		return job.Spec.Extensions.Armada
	}
	return map[string]interface{}{}
}

func stringExt(ext map[string]interface{}, key string) string {
	if v, ok := ext[key].(string); ok {
		return v
	}
	return ""
}

func ingressExt(ext map[string]interface{}) []armadatypes.Ingress {
	v, ok := ext["ingress"]
	if !ok {
		return nil
	}
	var out []armadatypes.Ingress
	if err := k8senc.ConvertVia(v, &out); err != nil {
		return nil
	}
	return out
}

func servicesExt(ext map[string]interface{}) []armadatypes.Service {
	v, ok := ext["services"]
	if !ok {
		return nil
	}
	var out []armadatypes.Service
	if err := k8senc.ConvertVia(v, &out); err != nil {
		return nil
	}
	return out
}

func floatValue(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}
