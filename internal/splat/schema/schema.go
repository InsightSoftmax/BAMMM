// Package schema generates the JSON Schema for a SPLAT Job directly from the Go
// types in internal/splat, so the published schema/splat.schema.json can never
// silently drift from the code. Regenerate with:
//
//	go test ./internal/splat/schema -update
package schema

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/invopop/jsonschema"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// SchemaID is the canonical $id of the SPLAT JSON Schema.
const SchemaID = "https://bammm.io/schemas/splat/v1alpha1.json"

// Generate reflects splat.Job into an indented JSON Schema document.
func Generate() ([]byte, error) {
	r := &jsonschema.Reflector{
		// The opaque scalar wrappers marshal to strings; reflection alone would
		// emit empty objects for them (they have only unexported fields).
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			switch t {
			case reflect.TypeOf(splat.Quantity{}):
				return &jsonschema.Schema{
					Type:        "string",
					Description: `Resource quantity, e.g. "4Gi", "4G", "4096M", "4096" (bare = MB), or "500m" (CPU millicores).`,
				}
			case reflect.TypeOf(splat.Duration{}):
				return &jsonschema.Schema{
					Type:        "string",
					Description: `Duration as ISO 8601 (e.g. PT2H30M), HH:MM:SS, or a plain number of seconds.`,
				}
			}
			return nil
		},
	}

	s := r.Reflect(&splat.Job{})
	s.ID = jsonschema.ID(SchemaID)
	s.Title = "SPLAT Job"
	s.Description = "Scheduler-Portable Language for Abstracting Tasks — the BAMMM interchange job spec " +
		"(apiVersion bammm.io/v1alpha1). Generated from the Go types in internal/splat; see SPEC.md."

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
