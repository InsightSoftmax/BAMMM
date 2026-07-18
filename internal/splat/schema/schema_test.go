package schema

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "regenerate schema/splat.schema.json")

// schemaPath is the committed schema, relative to this package directory.
const schemaPath = "../../../schema/splat.schema.json"

// TestSchemaMatchesTypes regenerates the schema from the Go types and, unless
// -update is set, fails if the committed file is out of date — the CI drift gate.
func TestSchemaMatchesTypes(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if *update {
		if err := os.MkdirAll("../../../schema", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(schemaPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", schemaPath)
		return
	}

	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s (generate it with `go test ./internal/splat/schema -update`): %v", schemaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("schema/splat.schema.json is stale — regenerate with `go test ./internal/splat/schema -update`")
	}
}

// TestSchemaShape sanity-checks that reflection produced a usable schema.
func TestSchemaShape(t *testing.T) {
	data, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v", err)
	}
	if doc["$id"] != SchemaID {
		t.Errorf("$id: got %v want %q", doc["$id"], SchemaID)
	}
	// The Job type reflects to a $ref into $defs; ensure the definitions exist
	// and carry the top-level SPLAT fields.
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("no $defs in generated schema")
	}
	job, ok := defs["Job"].(map[string]any)
	if !ok {
		t.Fatal("no Job definition in $defs")
	}
	props, ok := job["properties"].(map[string]any)
	if !ok {
		t.Fatal("Job has no properties")
	}
	for _, field := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Job schema missing property %q", field)
		}
	}
}
