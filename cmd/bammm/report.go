package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/InsightSoftmax/BAMMM/internal/convert"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// feature is a named SPLAT capability we tally across a set of parsed sources,
// to show what a corpus actually uses.
type feature struct {
	name    string
	present func(*splat.Job) bool
}

var features = []feature{
	{"queue/partition", func(j *splat.Job) bool { return j.Spec.Schedule.Queue != "" || j.Spec.Schedule.Partition != "" }},
	{"account/qos", func(j *splat.Job) bool { return j.Spec.Schedule.Account != "" || j.Spec.Schedule.QOS != "" }},
	{"priority", func(j *splat.Job) bool { return j.Spec.Schedule.Priority != 0 || j.Spec.Schedule.PriorityClass != "" }},
	{"walltime", func(j *splat.Job) bool { return j.Spec.Schedule.Walltime != nil }},
	{"nodes", func(j *splat.Job) bool { return j.Spec.Resources.Nodes > 0 }},
	{"cpus", func(j *splat.Job) bool { return j.Spec.Resources.CPUsPerTask > 0 }},
	{"memory", func(j *splat.Job) bool {
		return j.Spec.Resources.MemoryPerTask != nil || j.Spec.Resources.MemoryPerCPU != nil
	}},
	{"gpu", func(j *splat.Job) bool { return j.Spec.Resources.GPU != nil }},
	{"gang", func(j *splat.Job) bool { return j.Spec.Gang != nil }},
	{"array", func(j *splat.Job) bool { return j.Spec.Array != nil }},
	{"multi-task", func(j *splat.Job) bool { return len(j.Spec.Tasks) > 0 }},
	{"dependencies", func(j *splat.Job) bool { return len(j.Spec.Dependencies) > 0 }},
	{"notifications", func(j *splat.Job) bool { return j.Spec.Notifications != nil }},
	{"volumes", func(j *splat.Job) bool { return len(j.Spec.Volumes) > 0 }},
	{"output redirect", func(j *splat.Job) bool { return j.Spec.Output.Stdout != "" || j.Spec.Output.Stderr != "" }},
	{"container exec", func(j *splat.Job) bool { return j.Spec.Execution.Container != nil || hasContainerTask(j) }},
	{"script exec", func(j *splat.Job) bool { return j.Spec.Execution.Script != "" }},
}

func hasContainerTask(j *splat.Job) bool {
	for _, t := range j.Spec.Tasks {
		if t.Execution != nil && t.Execution.Container != nil {
			return true
		}
	}
	return false
}

// coverageReport aggregates SPLAT feature usage across parsed sources.
type coverageReport struct {
	parsed      int
	parseErrors int
	featureHits map[string]int
	extHits     map[string]int // scheduler namespace -> jobs using extensions.<ns>
}

func buildReport(items []inputItem, from string) coverageReport {
	rep := coverageReport{
		featureHits: map[string]int{},
		extHits:     map[string]int{},
	}
	for _, it := range items {
		data, err := os.ReadFile(it.path)
		if err != nil {
			rep.parseErrors++
			continue
		}
		job, err := convert.Parse(data, from)
		if err != nil {
			rep.parseErrors++
			continue
		}
		rep.parsed++
		for _, f := range features {
			if f.present(job) {
				rep.featureHits[f.name]++
			}
		}
		for ns, used := range extensionNamespaces(job) {
			if used {
				rep.extHits[ns]++
			}
		}
	}
	return rep
}

// extensionNamespaces reports which scheduler passthrough blocks a job uses.
// Anything landing here is information that did not map to a first-class SPLAT
// field — a signal of potential translation loss.
func extensionNamespaces(j *splat.Job) map[string]bool {
	e := j.Spec.Extensions
	return map[string]bool{
		"slurm":    len(e.Slurm) > 0,
		"pbs":      len(e.PBS) > 0,
		"lsf":      len(e.LSF) > 0,
		"htcondor": len(e.HTCondor) > 0,
		"flux":     len(e.Flux) > 0,
		"armada":   len(e.Armada) > 0,
		"volcano":  len(e.Volcano) > 0,
		"kueue":    len(e.Kueue) > 0,
		"yunikorn": len(e.YuniKorn) > 0,
		"runai":    len(e.RunAI) > 0,
	}
}

// writeReport parses every input as `from` and prints a coverage report.
func writeReport(w io.Writer, items []inputItem, from string) {
	rep := buildReport(items, from)

	fmt.Fprintf(w, "\nCoverage report (%s sources)\n", from)
	fmt.Fprintf(w, "  parsed: %d", rep.parsed)
	if rep.parseErrors > 0 {
		fmt.Fprintf(w, ", unparseable: %d", rep.parseErrors)
	}
	fmt.Fprintln(w)
	if rep.parsed == 0 {
		return
	}

	fmt.Fprintln(w, "\n  SPLAT field usage:")
	for _, f := range features { // stable order
		n := rep.featureHits[f.name]
		fmt.Fprintf(w, "    %-16s %3d/%-3d  %s\n", f.name, n, rep.parsed, bar(n, rep.parsed))
	}

	if len(rep.extHits) > 0 {
		fmt.Fprintln(w, "\n  extensions.* passthrough (potential translation loss):")
		for _, ns := range sortedKeys(rep.extHits) {
			fmt.Fprintf(w, "    %-16s %3d/%-3d\n", ns, rep.extHits[ns], rep.parsed)
		}
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// bar renders a fixed-width proportion bar.
func bar(n, total int) string {
	const width = 20
	if total == 0 {
		return ""
	}
	filled := n * width / total
	out := make([]byte, width)
	for i := range out {
		if i < filled {
			out[i] = '#'
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
