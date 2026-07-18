// Package armada parses Armada JobSubmitRequest YAML into SPLAT jobs.
// It is the inverse of internal/emitter/armada.
package armada

import (
	"fmt"
	"math"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	armadatypes "github.com/InsightSoftmax/BAMMM/internal/armada"
	"github.com/InsightSoftmax/BAMMM/internal/k8senc"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("armada", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts an Armada JobSubmitRequest into a SPLAT job. Each Armada job
// (pod) becomes a SPLAT task; gang annotations become spec.gang.
func Parse(data []byte) (*splat.Job, error) {
	var req armadatypes.Request
	if err := yaml.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("armada: unmarshal request: %w", err)
	}
	if len(req.Jobs) == 0 {
		return nil, fmt.Errorf("armada: request has no jobs")
	}

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Name = req.JobSetID
	if job.Metadata.Name == "" {
		job.Metadata.Name = req.Queue
	}
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "armada"}

	job.Spec.Schedule.Queue = req.Queue

	// ── Per-job tasks + aggregate schedule/metadata ──────────────────────────
	var maxPriority float64
	for i := range req.Jobs {
		aj := &req.Jobs[i]
		if aj.Priority > maxPriority {
			maxPriority = aj.Priority
		}
		if aj.ExternalJobURI != "" {
			job.Metadata.Annotations["bammm.io/external-uri"] = aj.ExternalJobURI
		}
		if aj.PodSpec != nil && aj.PodSpec.PriorityClassName != "" {
			job.Spec.Schedule.PriorityClass = aj.PodSpec.PriorityClassName
		}
		job.Spec.Tasks = append(job.Spec.Tasks, taskFromJob(aj, i))
	}
	if maxPriority > 0 {
		job.Spec.Schedule.Priority = splat.ArmadaPriority.Normalize(int(math.Round(maxPriority)))
	}

	job.Metadata.Labels = commonLabels(req.Jobs)
	applyGang(job, req.Jobs)
	applyExtensions(job, &req, maxPriority)

	return job, nil
}

// taskFromJob maps one Armada job (pod) to a SPLAT task.
func taskFromJob(aj *armadatypes.Job, index int) splat.Task {
	task := splat.Task{
		Name:     taskName(aj, index),
		Replicas: 1,
	}
	if aj.PodSpec == nil || len(aj.PodSpec.Containers) == 0 {
		return task
	}
	c := aj.PodSpec.Containers[0]

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

	if len(aj.PodSpec.Tolerations) > 0 {
		task.Placement = &splat.Placement{Tolerations: tolerations(aj.PodSpec.Tolerations)}
	}
	return task
}

func taskName(aj *armadatypes.Job, index int) string {
	if c := aj.Labels["component"]; c != "" {
		return c
	}
	if aj.ClientID != "" {
		return aj.ClientID
	}
	return fmt.Sprintf("task-%d", index)
}

// commonLabels returns labels present with the same value across all jobs
// (per-job differences such as "component" are dropped from job-level metadata).
func commonLabels(jobs []armadatypes.Job) map[string]string {
	if len(jobs) == 0 {
		return nil
	}
	common := map[string]string{}
	for k, v := range jobs[0].Labels {
		common[k] = v
	}
	for _, j := range jobs[1:] {
		for k, v := range common {
			if j.Labels[k] != v {
				delete(common, k)
			}
		}
	}
	if len(common) == 0 {
		return nil
	}
	return common
}

// applyGang derives spec.gang from the Armada gang annotations.
func applyGang(job *splat.Job, jobs []armadatypes.Job) {
	for _, j := range jobs {
		if j.Annotations[armadatypes.AnnGangID] == "" {
			continue
		}
		minSize := j.Annotations[armadatypes.AnnGangMinJobSize]
		if minSize == "" {
			minSize = j.Annotations[armadatypes.AnnGangCardinality]
		}
		n, err := strconv.Atoi(minSize)
		if err != nil || n == 0 {
			n = len(jobs)
		}
		job.Spec.Gang = &splat.Gang{MinAvailable: n, Style: splat.GangStyleHard}
		return
	}
}

// applyExtensions stashes Armada-specific fields for round-trip fidelity.
func applyExtensions(job *splat.Job, req *armadatypes.Request, rawPriority float64) {
	ext := map[string]interface{}{}
	if req.JobSetID != "" {
		ext["job_set_id"] = req.JobSetID
	}
	if len(req.Jobs) > 0 {
		if ns := req.Jobs[0].Namespace; ns != "" {
			ext["namespace"] = ns
		}
		if sched := req.Jobs[0].Scheduler; sched != "" {
			ext["scheduler_name"] = sched
		}
		if gid := req.Jobs[0].Annotations[armadatypes.AnnGangID]; gid != "" {
			ext["gang_id"] = gid
		}
	}
	if rawPriority > 0 {
		ext["priority"] = rawPriority
	}
	if len(req.Ingress) > 0 {
		ext["ingress"] = req.Ingress
	}
	if len(req.Services) > 0 {
		ext["services"] = req.Services
	}
	if len(ext) > 0 {
		job.Spec.Extensions.Armada = ext
	}
}

func tolerations(ts []corev1.Toleration) []interface{} {
	out := make([]interface{}, 0, len(ts))
	for i := range ts {
		out = append(out, ts[i])
	}
	return out
}
