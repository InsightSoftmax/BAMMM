// Package all imports every registered parser so that a single blank import
// in the cmd package is enough to activate all format support.
package all

import (
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/armada"
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/kueue"
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/slurm"
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/volcano"
)
