// Package pbs parses OpenPBS / PBS Pro job scripts (#PBS directives) into SPLAT
// jobs. It handles the modern `-l select=` chunk syntax and the legacy
// `-l nodes=…:ppn=…` form.
package pbs

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("pbs", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts a PBS batch script into a SPLAT job.
func Parse(data []byte) (*splat.Job, error) {
	directives, script, err := extract(data)
	if err != nil {
		return nil, err
	}
	if len(directives) == 0 {
		return nil, fmt.Errorf("pbs: no #PBS directives found")
	}

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "pbs"}

	for _, d := range directives {
		if err := apply(job, d.flag, d.value); err != nil {
			return nil, fmt.Errorf("pbs: directive %q: %w", d.raw, err)
		}
	}

	job.Spec.Execution.Script = script
	return job, nil
}

type directive struct {
	flag, value, raw string
}

// extract scans the script for #PBS lines and returns them plus the body.
func extract(data []byte) ([]directive, string, error) {
	var directives []directive
	var body []string
	sawDirective := false

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#PBS") {
			sawDirective = true
			rest := stripInlineComment(strings.TrimSpace(trimmed[len("#PBS"):]))
			if rest == "" {
				continue
			}
			flag, value := splitDirective(rest)
			directives = append(directives, directive{flag: flag, value: value, raw: trimmed})
			continue
		}
		// Keep the shebang and everything from the first non-directive line
		// after the header onward as the script body.
		if sawDirective || !strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "#!") {
			body = append(body, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return directives, strings.TrimSpace(strings.Join(body, "\n")), nil
}

// stripInlineComment removes a trailing " #comment" (whitespace + hash) from a
// PBS directive line, e.g. "-l nodes=1:ppn=40   # request one node".
func stripInlineComment(s string) string {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// splitDirective splits "-l select=1:ncpus=8" into flag "-l" and the remainder.
func splitDirective(rest string) (flag, value string) {
	if !strings.HasPrefix(rest, "-") {
		return rest, ""
	}
	// Flag is "-" plus one letter; the value is the rest (after optional space).
	flag = rest[:2]
	value = strings.TrimSpace(rest[2:])
	return flag, value
}

func apply(job *splat.Job, flag, value string) error {
	s := &job.Spec.Schedule
	switch flag {
	case "-N":
		job.Metadata.Name = value
	case "-q":
		s.Queue = value
	case "-A":
		s.Account = value
	case "-P":
		s.Project = value
	case "-p":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("priority: %w", err)
		}
		s.Priority = splat.PBSPriority.Normalize(n)
	case "-J":
		job.Spec.Array = parseArray(value)
	case "-l":
		// A single -l may carry a comma-separated resource list, e.g.
		// "-l mem=250gb,walltime=18:00:00,nodes=1:ppn=4".
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if err := applyResource(job, part); err != nil {
				return err
			}
		}
		return nil
	case "-o":
		job.Spec.Output.Stdout = normalizePath(value)
	case "-e":
		job.Spec.Output.Stderr = normalizePath(value)
	case "-j":
		switch value {
		case "oe", "eo":
			job.Spec.Output.MergeStderr = true
		}
	case "-M":
		ensureNotifications(job).Email = value
	case "-m":
		applyMail(job, value)
	case "-r":
		// Rerunnable: 'y' means requeue on failure.
		job.Spec.Lifecycle.RequeueOnFailure = value == "y"
	case "-h":
		s.Hold = true
	case "-W":
		return applyW(job, value)
	case "-V", "-v", "-S", "-k", "-d", "-e ", "-o ":
		// env/shell/keep/workdir — not modeled
	default:
		setPBS(job, sanitizeKey(flag), value)
	}
	return nil
}

// applyResource handles "-l key=value" (walltime, select, place, nodes, mem…).
func applyResource(job *splat.Job, value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok {
		setPBS(job, "l_"+sanitizeKey(value), "")
		return nil
	}
	r := &job.Spec.Resources
	switch key {
	case "walltime":
		d, err := parseDuration(val)
		if err != nil {
			return err
		}
		job.Spec.Schedule.Walltime = d
	case "select":
		return parseSelect(job, val)
	case "nodes":
		return parseNodes(job, val)
	case "ncpus":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("ncpus: %w", err)
		}
		r.CPUsPerTask = n
	case "mpiprocs":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("mpiprocs: %w", err)
		}
		r.TasksPerNode = n
	case "mem":
		q, err := parseMemory(val)
		if err != nil {
			return err
		}
		r.MemoryPerTask = q
	case "ngpus":
		g, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("ngpus: %w", err)
		}
		r.GPU = &splat.GPURequest{Count: g}
	case "place":
		applyPlace(job, val)
	default:
		setPBS(job, "l_"+sanitizeKey(key), val)
	}
	return nil
}

// parseSelect parses "N:ncpus=8:mem=128gb:ngpus=1:scratch=500gb:ib=true".
func parseSelect(job *splat.Job, val string) error {
	r := &job.Spec.Resources
	parts := strings.Split(val, ":")
	for i, p := range parts {
		if i == 0 {
			if n, err := strconv.Atoi(p); err == nil {
				r.Nodes = n
				continue
			}
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "ncpus":
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("select ncpus: %w", err)
			}
			r.CPUsPerTask = n
		case "mpiprocs":
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("select mpiprocs: %w", err)
			}
			r.TasksPerNode = n
		case "mem":
			q, err := parseMemory(v)
			if err != nil {
				return err
			}
			r.MemoryPerTask = q
		case "ngpus":
			g, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("select ngpus: %w", err)
			}
			r.GPU = &splat.GPURequest{Count: g}
		case "scratch", "scratch_local", "scratch_ssd":
			q, err := parseMemory(v)
			if err != nil {
				return err
			}
			r.DiskPerTask = q
		default:
			// Site-specific chunk resources (e.g. ib=true) — preserve.
			setPBS(job, "select_"+sanitizeKey(k), v)
		}
	}
	if r.Nodes == 0 {
		r.Nodes = 1
	}
	return nil
}

// parseNodes parses the legacy "N:ppn=C" form.
func parseNodes(job *splat.Job, val string) error {
	r := &job.Spec.Resources
	parts := strings.Split(val, ":")
	if n, err := strconv.Atoi(parts[0]); err == nil {
		r.Nodes = n
	}
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		if k == "ppn" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("ppn: %w", err)
			}
			r.TasksPerNode = n
		}
	}
	return nil
}

func applyPlace(job *splat.Job, val string) {
	for _, tok := range strings.Split(val, ":") {
		switch tok {
		case "scatter":
			job.Spec.Placement.Topology = splat.TopologyScatter
		case "pack":
			job.Spec.Placement.Topology = splat.TopologyPack
		case "free":
			job.Spec.Placement.Topology = splat.TopologyFree
		}
	}
}

// applyW handles "-W depend=…" (dependencies) and other -W attributes.
func applyW(job *splat.Job, value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok {
		setPBS(job, "w_"+sanitizeKey(value), "")
		return nil
	}
	if key == "depend" {
		job.Spec.Dependencies = append(job.Spec.Dependencies, parseDepend(val)...)
		return nil
	}
	setPBS(job, "w_"+sanitizeKey(key), val)
	return nil
}

// parseDepend parses "afterok:123:456,afterany:789".
func parseDepend(val string) []splat.Dependency {
	var deps []splat.Dependency
	for _, clause := range strings.Split(val, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		parts := strings.Split(clause, ":")
		scheme := splat.DependencyScheme(parts[0])
		if len(parts) == 1 {
			deps = append(deps, splat.Dependency{Scheme: scheme})
			continue
		}
		for _, id := range parts[1:] {
			if id != "" {
				deps = append(deps, splat.Dependency{Scheme: scheme, Value: id})
			}
		}
	}
	return deps
}

func parseArray(val string) *splat.Array {
	a := &splat.Array{}
	if idx := strings.IndexByte(val, '%'); idx >= 0 {
		if n, err := strconv.Atoi(val[idx+1:]); err == nil {
			a.MaxConcurrent = n
		}
		val = val[:idx]
	}
	a.Indices = val
	return a
}

func applyMail(job *splat.Job, value string) {
	n := ensureNotifications(job)
	for _, ch := range value {
		switch ch {
		case 'a':
			n.Events = append(n.Events, splat.NotifyFail)
		case 'b':
			n.Events = append(n.Events, splat.NotifyBegin)
		case 'e':
			n.Events = append(n.Events, splat.NotifyEnd)
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func ensureNotifications(job *splat.Job) *splat.Notifications {
	if job.Spec.Notifications == nil {
		job.Spec.Notifications = &splat.Notifications{}
	}
	return job.Spec.Notifications
}

func setPBS(job *splat.Job, key, value string) {
	if job.Spec.Extensions.PBS == nil {
		job.Spec.Extensions.PBS = map[string]interface{}{}
	}
	job.Spec.Extensions.PBS[key] = value
}

func sanitizeKey(s string) string {
	return strings.ReplaceAll(strings.TrimPrefix(strings.ToLower(s), "-"), "-", "_")
}

// normalizePath maps PBS filename tokens to SPLAT template variables.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "%J", "{job_id}")
	p = strings.ReplaceAll(p, "%I", "{array_index}")
	p = strings.ReplaceAll(p, "%a", "{array_index}")
	return p
}

func parseDuration(s string) (*splat.Duration, error) {
	var d splat.Duration
	if err := d.UnmarshalJSON([]byte(`"` + s + `"`)); err != nil {
		return nil, fmt.Errorf("walltime %q: %w", s, err)
	}
	return &d, nil
}

// parseMemory parses PBS memory strings. PBS suffixes are powers of 1024 and
// case-insensitive, with the trailing "b" optional: 128gb / 128GB / 128g / 128G
// all mean 128 GiB. A bare number is bytes (PBS's default unit).
func parseMemory(s string) (*splat.Quantity, error) {
	lower := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "b")
	for _, pair := range [][2]string{{"t", "Ti"}, {"g", "Gi"}, {"m", "Mi"}, {"k", "Ki"}} {
		if strings.HasSuffix(lower, pair[0]) {
			iec := strings.TrimSuffix(lower, pair[0]) + pair[1]
			var q splat.Quantity
			if err := q.UnmarshalJSON([]byte(`"` + iec + `"`)); err != nil {
				return nil, fmt.Errorf("memory %q: %w", s, err)
			}
			return &q, nil
		}
	}
	// No unit suffix: PBS treats a bare number as bytes.
	n, err := strconv.ParseInt(lower, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("memory %q: %w", s, err)
	}
	return splat.QuantityOf(n), nil
}
