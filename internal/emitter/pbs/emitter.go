// Package pbs emits OpenPBS / PBS Pro job scripts (#PBS directives) from SPLAT
// jobs. It is the inverse of internal/parser/pbs.
package pbs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("pbs", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// Emit converts a SPLAT job into a PBS batch script.
func Emit(job *splat.Job) ([]byte, error) {
	var b strings.Builder

	shell := job.Spec.Execution.Shell
	if shell == "" {
		shell = "/bin/bash"
	}
	if strings.HasPrefix(shell, "/") {
		fmt.Fprintf(&b, "#!%s\n", shell)
	} else {
		fmt.Fprintf(&b, "#!/usr/bin/env %s\n", shell)
	}

	for _, line := range directives(job) {
		fmt.Fprintf(&b, "#PBS %s\n", line)
	}

	body := bodyFor(job)
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String()), nil
}

func directives(job *splat.Job) []string {
	var out []string
	s := job.Spec.Schedule
	r := job.Spec.Resources

	if job.Metadata.Name != "" {
		out = append(out, "-N "+job.Metadata.Name)
	}
	if s.Queue != "" {
		out = append(out, "-q "+s.Queue)
	} else if s.Partition != "" {
		out = append(out, "-q "+s.Partition)
	}
	if s.Account != "" {
		out = append(out, "-A "+s.Account)
	}
	if s.Project != "" {
		out = append(out, "-P "+s.Project)
	}
	if s.Priority != 0 {
		out = append(out, "-p "+strconv.Itoa(splat.PBSPriority.Denormalize(s.Priority)))
	}
	if a := job.Spec.Array; a != nil && a.Indices != "" {
		val := a.Indices
		if a.MaxConcurrent > 0 {
			val = fmt.Sprintf("%s%%%d", val, a.MaxConcurrent)
		}
		out = append(out, "-J "+val)
	}
	if sel := selectStatement(job); sel != "" {
		out = append(out, "-l select="+sel)
	}
	if p := placeStatement(job); p != "" {
		out = append(out, "-l place="+p)
	}
	if s.Walltime != nil {
		out = append(out, "-l walltime="+s.Walltime.String())
	}
	if job.Spec.Output.Stdout != "" {
		out = append(out, "-o "+denormalizePath(job.Spec.Output.Stdout))
	}
	if job.Spec.Output.Stderr != "" {
		out = append(out, "-e "+denormalizePath(job.Spec.Output.Stderr))
	}
	if job.Spec.Output.MergeStderr {
		out = append(out, "-j oe")
	}
	if n := job.Spec.Notifications; n != nil {
		if m := mailEvents(n.Events); m != "" {
			out = append(out, "-m "+m)
		}
		if n.Email != "" {
			out = append(out, "-M "+n.Email)
		}
	}
	if job.Spec.Lifecycle.RequeueOnFailure {
		out = append(out, "-r y")
	}
	if s.Hold {
		out = append(out, "-h")
	}
	if dep := dependStatement(job.Spec.Dependencies); dep != "" {
		out = append(out, "-W depend="+dep)
	}
	_ = r
	out = append(out, extensionDirectives(job.Spec.Extensions.PBS)...)
	return out
}

// selectStatement builds the "-l select=" chunk from resources plus any
// site-specific chunk resources preserved in extensions (select_*).
func selectStatement(job *splat.Job) string {
	r := job.Spec.Resources
	extras := selectExtras(job.Spec.Extensions.PBS)
	if r.Nodes == 0 && r.CPUsPerTask == 0 && r.MemoryPerTask == nil && r.GPU == nil &&
		r.DiskPerTask == nil && r.TasksPerNode == 0 && len(extras) == 0 {
		return ""
	}
	nodes := r.Nodes
	if nodes == 0 {
		nodes = 1
	}
	parts := []string{strconv.Itoa(nodes)}
	if r.CPUsPerTask > 0 {
		parts = append(parts, "ncpus="+strconv.Itoa(r.CPUsPerTask))
	}
	if r.TasksPerNode > 0 {
		parts = append(parts, "mpiprocs="+strconv.Itoa(r.TasksPerNode))
	}
	if r.MemoryPerTask != nil {
		parts = append(parts, "mem="+pbsMem(r.MemoryPerTask))
	}
	if r.GPU != nil && r.GPU.Count > 0 {
		parts = append(parts, fmt.Sprintf("ngpus=%d", int(r.GPU.Count+0.5)))
	}
	if r.DiskPerTask != nil {
		parts = append(parts, "scratch="+pbsMem(r.DiskPerTask))
	}
	parts = append(parts, extras...)
	return strings.Join(parts, ":")
}

func placeStatement(job *splat.Job) string {
	switch job.Spec.Placement.Topology {
	case splat.TopologyScatter:
		return "scatter"
	case splat.TopologyPack:
		return "pack"
	case splat.TopologyFree:
		return "free"
	}
	return ""
}

// selectExtras pulls the sorted site-specific chunk resources (select_*).
func selectExtras(ext map[string]interface{}) []string {
	var keys []string
	for k := range ext {
		if strings.HasPrefix(k, "select_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		name := strings.TrimPrefix(k, "select_")
		v, _ := scalarString(ext[k])
		if v == "" {
			out = append(out, name)
		} else {
			out = append(out, name+"="+v)
		}
	}
	return out
}

// extensionDirectives renders PBS passthrough entries (l_*, w_*, and unknown
// single-letter flags); select_* is folded into the select statement above.
func extensionDirectives(ext map[string]interface{}) []string {
	if len(ext) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []string
	for _, k := range keys {
		if strings.HasPrefix(k, "select_") {
			continue
		}
		v, ok := scalarString(ext[k])
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(k, "l_"):
			out = append(out, resourceDirective("-l", strings.TrimPrefix(k, "l_"), v))
		case strings.HasPrefix(k, "w_"):
			out = append(out, resourceDirective("-W", strings.TrimPrefix(k, "w_"), v))
		default:
			// Unknown single-letter flag, e.g. key "x" -> "-x value".
			flag := "-" + k
			if v == "" {
				out = append(out, flag)
			} else {
				out = append(out, flag+" "+v)
			}
		}
	}
	return out
}

func resourceDirective(flag, key, val string) string {
	if val == "" {
		return fmt.Sprintf("%s %s", flag, key)
	}
	return fmt.Sprintf("%s %s=%s", flag, key, val)
}

func bodyFor(job *splat.Job) string {
	e := job.Spec.Execution
	if e.Script != "" {
		return stripLeadingShebang(e.Script)
	}
	if e.Executable != "" {
		cmd := e.Executable
		if e.Arguments != "" {
			cmd += " " + e.Arguments
		}
		return cmd
	}
	if e.Container != nil && e.Container.Image != "" {
		uri := e.Container.Image
		if !strings.Contains(uri, "://") {
			uri = "docker://" + uri
		}
		return strings.TrimSpace("singularity exec " + uri + " " + strings.Join(e.Container.Command, " "))
	}
	return ""
}

func stripLeadingShebang(body string) string {
	if !strings.HasPrefix(body, "#!") {
		return body
	}
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		return strings.TrimLeft(body[nl+1:], "\n")
	}
	return ""
}

// pbsMem renders a Quantity as a PBS memory string (kb/mb/gb/tb, powers of 1024).
func pbsMem(q *splat.Quantity) string {
	iec := q.String() // e.g. "128Gi"
	suffixes := [][2]string{{"Ti", "tb"}, {"Gi", "gb"}, {"Mi", "mb"}, {"Ki", "kb"}}
	for _, p := range suffixes {
		if strings.HasSuffix(iec, p[0]) {
			return strings.TrimSuffix(iec, p[0]) + p[1]
		}
	}
	// Fall back to bytes.
	return strconv.FormatInt(q.Bytes(), 10) + "b"
}

func mailEvents(events []splat.NotificationEvent) string {
	var b strings.Builder
	for _, e := range events {
		switch e {
		case splat.NotifyFail:
			b.WriteString("a")
		case splat.NotifyBegin:
			b.WriteString("b")
		case splat.NotifyEnd:
			b.WriteString("e")
		}
	}
	return b.String()
}

func dependStatement(deps []splat.Dependency) string {
	if len(deps) == 0 {
		return ""
	}
	var order []splat.DependencyScheme
	values := map[splat.DependencyScheme][]string{}
	for _, d := range deps {
		if _, seen := values[d.Scheme]; !seen {
			order = append(order, d.Scheme)
		}
		if d.Value != "" {
			values[d.Scheme] = append(values[d.Scheme], d.Value)
		} else if _, seen := values[d.Scheme]; !seen {
			values[d.Scheme] = nil
		}
	}
	clauses := make([]string, 0, len(order))
	for _, scheme := range order {
		vals := values[scheme]
		if len(vals) == 0 {
			clauses = append(clauses, string(scheme))
			continue
		}
		clauses = append(clauses, string(scheme)+":"+strings.Join(vals, ":"))
	}
	return strings.Join(clauses, ",")
}

func denormalizePath(p string) string {
	p = strings.ReplaceAll(p, "{job_id}", "%J")
	p = strings.ReplaceAll(p, "{array_index}", "%I")
	return p
}

func scalarString(v interface{}) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, true
	case bool, int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", t), true
	default:
		return "", false
	}
}
