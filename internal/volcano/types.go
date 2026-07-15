// Package volcano defines the wire structs for a Volcano vcjob
// (batch.volcano.sh/v1alpha1 Job). Task templates are standard core/v1
// PodTemplateSpecs, so only the surrounding envelope is hand-rolled.
package volcano

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// APIVersion / Kind for a Volcano job.
const (
	APIVersion = "batch.volcano.sh/v1alpha1"
	Kind       = "Job"
)

// Job is a Volcano vcjob.
type Job struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JobSpec `json:"spec,omitempty"`
}

// JobSpec is the vcjob spec.
type JobSpec struct {
	SchedulerName           string              `json:"schedulerName,omitempty"`
	Queue                   string              `json:"queue,omitempty"`
	PriorityClassName       string              `json:"priorityClassName,omitempty"`
	MinAvailable            int32               `json:"minAvailable,omitempty"`
	MaxRetry                int32               `json:"maxRetry,omitempty"`
	TTLSecondsAfterFinished *int32              `json:"ttlSecondsAfterFinished,omitempty"`
	Plugins                 map[string][]string `json:"plugins,omitempty"`
	Policies                []Policy            `json:"policies,omitempty"`
	Tasks                   []Task              `json:"tasks,omitempty"`
}

// Task is one role in a vcjob.
type Task struct {
	Name         string                 `json:"name,omitempty"`
	Replicas     int32                  `json:"replicas,omitempty"`
	MinAvailable *int32                 `json:"minAvailable,omitempty"`
	Template     corev1.PodTemplateSpec `json:"template,omitempty"`
	Policies     []Policy               `json:"policies,omitempty"`
}

// Policy is an event→action rule.
type Policy struct {
	Event  string `json:"event,omitempty"`
	Action string `json:"action,omitempty"`
}
