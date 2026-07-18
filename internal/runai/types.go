// Package runai defines the wire structs for a Run.ai (NVIDIA) workload
// (run.ai/v2alpha1 TrainingWorkload). Run.ai wraps each spec field in a
// {value: ...} object; its distinctive capability is fractional GPU allocation.
package runai

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// APIVersion / Kind for a Run.ai training workload.
const (
	APIVersion = "run.ai/v2alpha1"
	Kind       = "TrainingWorkload"
)

// Workload is a Run.ai TrainingWorkload.
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              Spec `json:"spec,omitempty"`
}

// Spec is the workload spec. Every field is wrapped in {value: ...}.
type Spec struct {
	Image       *StringValue `json:"image,omitempty"`
	GPU         *StringValue `json:"gpu,omitempty"`       // fractional ("0.5") or whole ("2")
	GPUMemory   *StringValue `json:"gpuMemory,omitempty"` // e.g. "24G"
	CPU         *StringValue `json:"cpu,omitempty"`
	Memory      *StringValue `json:"memory,omitempty"`
	Command     *StringValue `json:"command,omitempty"`
	Arguments   *StringValue `json:"arguments,omitempty"`
	Environment *EnvItems    `json:"environment,omitempty"`
	Parallelism *IntValue    `json:"parallelism,omitempty"`
	Completions *IntValue    `json:"completions,omitempty"`
}

// StringValue is a Run.ai {value: "..."} field.
type StringValue struct {
	Value string `json:"value"`
}

// IntValue is a Run.ai {value: N} field.
type IntValue struct {
	Value int `json:"value"`
}

// EnvItems holds environment variables as {items: {KEY: {value: V}}}.
type EnvItems struct {
	Items map[string]StringValue `json:"items,omitempty"`
}
