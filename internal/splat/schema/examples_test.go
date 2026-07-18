package schema

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"
)

// TestExamplesValidate compiles the committed schema and validates every
// examples/*.yaml against it — the gate that keeps the docs' example specs
// honest (all fields camelCase and real, not silently dropped at parse time).
func TestExamplesValidate(t *testing.T) {
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(SchemaID, doc); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	sch, err := c.Compile(SchemaID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	files, err := filepath.Glob("../../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("%s: read: %v", f, err)
			continue
		}
		jsonb, err := yaml.YAMLToJSON(data)
		if err != nil {
			t.Errorf("%s: yaml→json: %v", f, err)
			continue
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonb))
		if err != nil {
			t.Errorf("%s: parse: %v", f, err)
			continue
		}
		if err := sch.Validate(inst); err != nil {
			t.Errorf("%s does not validate against the SPLAT schema:\n%v", filepath.Base(f), err)
		}
	}
}
