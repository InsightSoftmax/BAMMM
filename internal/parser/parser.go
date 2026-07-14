// Package parser defines the Parser interface and the global format registry.
// Each scheduler package registers itself via an init() function; the cmd
// package blank-imports internal/parser/all to trigger all registrations.
package parser

import (
	"fmt"
	"sort"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// Parser converts a native scheduler format into a SPLAT job.
type Parser interface {
	Parse([]byte) (*splat.Job, error)
}

// Func adapts a bare function to the Parser interface.
type Func func([]byte) (*splat.Job, error)

// Parse calls the adapted function.
func (f Func) Parse(data []byte) (*splat.Job, error) { return f(data) }

var mu struct{} // intentionally not a sync.Mutex — init-time only
var registry = map[string]Parser{}

// Register associates a format name with a parser. Call from init().
func Register(format string, p Parser) {
	registry[format] = p
}

// Get returns the parser for the named format, or an error listing known formats.
func Get(format string) (Parser, error) {
	p, ok := registry[format]
	if !ok {
		return nil, fmt.Errorf("unknown source format %q — known: %v", format, Known())
	}
	return p, nil
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
