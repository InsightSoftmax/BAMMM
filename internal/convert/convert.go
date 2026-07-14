// Package convert orchestrates a full parse → emit pipeline.
package convert

import (
	"fmt"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// Convert translates job spec bytes from one scheduler format to another.
// Use "splat" as from/to for SPLAT pass-through (useful for validation).
func Convert(input []byte, from, to string) ([]byte, error) {
	job, err := parse(input, from)
	if err != nil {
		return nil, err
	}
	return emit(job, to)
}

func parse(input []byte, from string) (*splat.Job, error) {
	if from == "splat" {
		return splat.Parse(input)
	}
	p, err := parser.Get(from)
	if err != nil {
		return nil, err
	}
	job, err := p.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", from, err)
	}
	return job, nil
}

func emit(job *splat.Job, to string) ([]byte, error) {
	if to == "splat" {
		return splat.Marshal(job)
	}
	e, err := emitter.Get(to)
	if err != nil {
		return nil, err
	}
	out, err := e.Emit(job)
	if err != nil {
		return nil, fmt.Errorf("emit %q: %w", to, err)
	}
	return out, nil
}
