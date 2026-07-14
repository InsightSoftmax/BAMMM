package splat

import "sigs.k8s.io/yaml"

const (
	APIVersion = "bammm.io/v1alpha1"
	Kind       = "Job"
)

// Parse unmarshals SPLAT YAML (or JSON) bytes into a Job.
func Parse(data []byte) (*Job, error) {
	var j Job
	if err := yaml.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// Marshal marshals a Job to SPLAT YAML.
func Marshal(j *Job) ([]byte, error) {
	j.APIVersion = APIVersion
	j.Kind = Kind
	return yaml.Marshal(j)
}
