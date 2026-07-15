// Package emitter defines the Emitter interface and the global format registry.
// Each scheduler package registers itself via an init() function; the cmd
// package blank-imports internal/emitter/all to trigger all registrations.
package emitter

import (
	"fmt"
	"sort"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// Emitter converts a SPLAT job into a native scheduler format.
type Emitter interface {
	Emit(*splat.Job) ([]byte, error)
}

// Func adapts a bare function to the Emitter interface.
type Func func(*splat.Job) ([]byte, error)

// Emit calls the adapted function.
func (f Func) Emit(j *splat.Job) ([]byte, error) { return f(j) }

var registry = map[string]Emitter{}

// Register associates a format name with an emitter. Call from init().
func Register(format string, e Emitter) {
	registry[format] = e
}

// Get returns the emitter for the named format, or an error listing known formats.
func Get(format string) (Emitter, error) {
	e, ok := registry[format]
	if !ok {
		return nil, fmt.Errorf("unknown target format %q — known: %v", format, Known())
	}
	return e, nil
}

// Known returns all registered format names in sorted order.
func Known() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
