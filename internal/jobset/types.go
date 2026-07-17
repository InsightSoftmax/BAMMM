// Package jobset defines the wire structs for a Kubernetes JobSet
// (jobset.x-k8s.io/v1alpha2), the multi-role primitive Kueue admits natively.
// Each role is a ReplicatedJob wrapping a standard batch/v1 Job template.
package jobset

import (
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// APIVersion / Kind for a JobSet.
const (
	APIVersion = "jobset.x-k8s.io/v1alpha2"
	Kind       = "JobSet"
)

// JobSet groups one or more ReplicatedJobs that are admitted together.
type JobSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JobSetSpec `json:"spec,omitempty"`
}

// JobSetSpec is the JobSet spec.
type JobSetSpec struct {
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty"`
}

// ReplicatedJob is one role: N identical copies of a batch/v1 Job template.
type ReplicatedJob struct {
	Name     string                  `json:"name"`
	Replicas int32                   `json:"replicas,omitempty"`
	Template batchv1.JobTemplateSpec `json:"template"`
}
