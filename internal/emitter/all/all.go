// Package all imports every registered emitter so that a single blank import
// in the cmd package is enough to activate all format support.
//
// Add one line per new emitter package:
//
//	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/slurm"
package all

import (
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/armada"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/htcondor"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/kueue"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/pbs"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/slurm"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/volcano"
)
