// Package armada defines the wire structs for an Armada JobSubmitRequest.
//
// Armada is normally driven over gRPC (armadactl / the Python client); this is
// the YAML/JSON projection of that protobuf message, which is what humans
// author and what BAMMM reads and writes. Pod specs are standard core/v1, so we
// only hand-roll the surrounding envelope.
package armada

import corev1 "k8s.io/api/core/v1"

// Request is the top-level Armada submission.
type Request struct {
	Queue    string    `json:"queue,omitempty"`
	JobSetID string    `json:"jobSetId,omitempty"`
	Jobs     []Job     `json:"jobs,omitempty"`
	Ingress  []Ingress `json:"ingress,omitempty"`
	Services []Service `json:"services,omitempty"`
}

// Job is one Armada job (one pod) within the request.
type Job struct {
	ClientID       string            `json:"clientId,omitempty"`
	Priority       float64           `json:"priority,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	Scheduler      string            `json:"scheduler,omitempty"`
	ExternalJobURI string            `json:"externalJobUri,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	PodSpec        *corev1.PodSpec   `json:"podSpec,omitempty"`
}

// Ingress exposes container ports via a Kubernetes Ingress.
type Ingress struct {
	ClientID    string            `json:"clientId,omitempty"`
	Ports       []int             `json:"ports,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	TLSEnabled  bool              `json:"tlsEnabled,omitempty"`
}

// Service exposes container ports via a Kubernetes Service.
type Service struct {
	ClientID string `json:"clientId,omitempty"`
	Type     string `json:"type,omitempty"`
	Ports    []int  `json:"ports,omitempty"`
	Name     string `json:"name,omitempty"`
}

// Armada gang-scheduling annotation keys.
const (
	AnnGangID          = "armadaproject.io/gang-id"
	AnnGangCardinality = "armadaproject.io/gang-cardinality"
	AnnGangMinJobSize  = "armadaproject.io/gang-minimum-job-size"
)
