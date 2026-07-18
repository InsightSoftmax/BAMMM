// Package slurm parses Slurm batch scripts (#SBATCH directives) into SPLAT jobs.
package slurm

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
	parser.Register("slurm", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts a Slurm batch script into a SPLAT job.
// It is exported so tests in external packages can call it directly.
func Parse(data []byte) (*splat.Job, error) {
	directives, script, err := extract(data)
	if err != nil {
		return nil, err
	}
	if len(directives) == 0 {
		return nil, fmt.Errorf("slurm: no #SBATCH directives found")
	}

	job := &splat.Job{
		APIVersion: splat.APIVersion,
		Kind:       splat.Kind,
	}
	job.Metadata.Annotations = map[string]string{
		"bammm.io/source-format": "slurm",
	}

	for _, d := range directives {
		if err := apply(job, d.flag, d.value); err != nil {
			if isShellExpr(d.value) {
				// Store unresolvable shell expressions verbatim in extensions.
				setSlurm(job, sanitizeKey(d.flag), d.value)
				continue
			}
			return nil, fmt.Errorf("slurm: directive %q: %w", d.raw, err)
		}
	}

	// Derived: total tasks = nodes × tasks_per_node (if not set explicitly)
	r := &job.Spec.Resources
	if r.Tasks == 0 && r.Nodes > 0 && r.TasksPerNode > 0 {
		r.Tasks = r.Nodes * r.TasksPerNode
	}

	job.Spec.Execution.Script = script
	return job, nil
}

// ── Extraction ────────────────────────────────────────────────────────────────

type directive struct {
	flag, value, raw string
}

// extract scans the script for #SBATCH lines and returns them as parsed
// directives plus the script body (everything below the header block).
func extract(data []byte) ([]directive, string, error) {
	var directives []directive
	var bodyLines []string
	inHeader := true

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#SBATCH") {
			inHeader = false
			rest := strings.TrimSpace(trimmed[len("#SBATCH"):])
			if rest == "" {
				continue
			}
			directives = append(directives, parseAllDirectives(rest)...)
			continue
		}

		// Lines before any #SBATCH (shebang, blank lines) are kept in body.
		// Lines after the first non-#SBATCH line post-header are the script body.
		if !inHeader || (len(directives) > 0 && !strings.HasPrefix(trimmed, "#")) {
			inHeader = false
		}
		if !inHeader || len(directives) == 0 {
			bodyLines = append(bodyLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}

	return directives, strings.TrimSpace(strings.Join(bodyLines, "\n")), nil
}

// parseAllDirectives splits one #SBATCH line into zero or more (flag, value) pairs.
// Slurm allows multiple flags on a single line: "#SBATCH --ntasks=1 --cpus-per-task 4".
func parseAllDirectives(rest string) []directive {
	// Strip inline comments: everything after whitespace+# (tab or spaces).
	if i := strings.Index(rest, "\t#"); i >= 0 {
		rest = rest[:i]
	}
	// Also strip " # " style comments (space + hash + space or end).
	if i := strings.Index(rest, " #"); i >= 0 {
		// Only strip if the # is followed by a space or end (not mid-word like #SBATCH).
		after := rest[i+2:]
		if after == "" || after[0] == ' ' || after[0] == '\t' {
			rest = rest[:i]
		}
	}
	rest = strings.TrimSpace(rest)

	var out []directive
	for rest != "" {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		var f, v string
		var consumed int
		f, v, consumed = nextFlag(rest)
		if consumed == 0 {
			break
		}
		out = append(out, directive{flag: f, value: v, raw: rest[:consumed]})
		rest = strings.TrimSpace(rest[consumed:])
	}
	return out
}

// nextFlag parses the next flag from s and returns (flag, value, bytesConsumed).
func nextFlag(s string) (flag, value string, n int) {
	if strings.HasPrefix(s, "--") {
		s2 := s[2:]
		// find end of flag name: '=', ' ', or end
		nameEnd := strings.IndexAny(s2, "= ")
		if nameEnd < 0 {
			// boolean flag at end of line
			return s2, "", len(s)
		}
		name := s2[:nameEnd]
		if s2[nameEnd] == '=' {
			// --flag=value: value ends at next ' --' or '-' boundary
			val, vlen := consumeValue(s2[nameEnd+1:])
			return name, val, 2 + nameEnd + 1 + vlen
		}
		// --flag value: value is next token (may be absent if next token starts with -)
		rest := strings.TrimSpace(s2[nameEnd+1:])
		if rest == "" || strings.HasPrefix(rest, "-") {
			return name, "", 2 + nameEnd + 1
		}
		val, vlen := consumeValue(rest)
		// account for leading spaces we trimmed
		leading := len(s2[nameEnd+1:]) - len(rest)
		return name, val, 2 + nameEnd + 1 + leading + vlen
	}
	if strings.HasPrefix(s, "-") && len(s) >= 2 && s[1] != '-' {
		name := string(s[1])
		rest := strings.TrimSpace(s[2:])
		if rest == "" || strings.HasPrefix(rest, "-") {
			return name, "", 2
		}
		val, vlen := consumeValue(rest)
		leading := len(s[2:]) - len(rest)
		return name, val, 2 + leading + vlen
	}
	// bare keyword (e.g. "hetjob")
	if !strings.HasPrefix(s, "-") {
		end := strings.IndexByte(s, ' ')
		if end < 0 {
			return s, "", len(s)
		}
		return s[:end], "", end
	}
	return "", "", 0
}

// consumeValue reads a value token, stopping before the next flag (-- or -letter).
func consumeValue(s string) (value string, n int) {
	// Walk forward; stop when we see whitespace followed by a flag marker.
	i := 0
	for i < len(s) {
		spaceAt := strings.IndexByte(s[i:], ' ')
		if spaceAt < 0 {
			return s, len(s)
		}
		spaceAt += i
		rest := strings.TrimSpace(s[spaceAt+1:])
		if strings.HasPrefix(rest, "--") || (len(rest) >= 2 && rest[0] == '-' && rest[1] != '-') {
			return s[:spaceAt], spaceAt
		}
		i = spaceAt + 1
	}
	return s, len(s)
}

// ── Directive → SPLAT mapping ─────────────────────────────────────────────────

func apply(job *splat.Job, flag, value string) error {
	s := &job.Spec.Schedule
	r := &job.Spec.Resources
	p := &job.Spec.Placement

	switch flag {
	// ── Identity / metadata
	case "job-name", "J":
		job.Metadata.Name = value

	// ── Queue / scheduling
	case "partition", "p":
		s.Partition = value
		s.Queue = value // mirror: queue and partition are aliases in SPLAT
	case "account", "A":
		s.Account = value
	case "qos", "q":
		s.QOS = value
	case "priority":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("priority: %w", err)
		}
		s.Priority = splat.SlurmPriority.Normalize(n)
	case "reservation":
		s.Reservation = value
	case "hold", "H":
		s.Hold = true
	case "oversubscribe", "s":
		s.Oversubscribe = true
	case "exclusive":
		s.Exclusive = true
		if value != "" {
			s.ExclusiveMode = value // exclusive=user / exclusive=mcs
		}
	case "requeue":
		job.Spec.Lifecycle.RequeueOnFailure = true
	case "no-requeue":
		job.Spec.Lifecycle.RequeueOnFailure = false

	// ── Timing
	case "time", "t":
		d, err := parseDuration(value)
		if err != nil {
			return err
		}
		s.Walltime = d
	case "time-min":
		d, err := parseDuration(value)
		if err != nil {
			return err
		}
		s.WalltimeMin = d
		setSlurm(job, "time_min", d.String())
	case "begin":
		// Store in extensions; time parsing is complex (relative + absolute)
		setSlurm(job, "begin", value)
	case "deadline":
		setSlurm(job, "deadline", value)

	// ── Resources: nodes / tasks
	case "nodes", "N":
		n, err := parseIntOrRange(value)
		if err != nil {
			return err
		}
		r.Nodes = n
	case "ntasks", "n":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("ntasks: %w", err)
		}
		r.Tasks = n
	case "ntasks-per-node", "tasks-per-node": // --tasks-per-node is a Slurm alias
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("ntasks-per-node: %w", err)
		}
		r.TasksPerNode = n
	case "ntasks-per-socket":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("ntasks-per-socket: %w", err)
		}
		r.TasksPerSocket = n
	case "ntasks-per-core":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("ntasks-per-core: %w", err)
		}
		r.TasksPerCore = n
	case "cpus-per-task", "c":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("cpus-per-task: %w", err)
		}
		r.CPUsPerTask = n

	// ── Resources: memory
	case "mem":
		q, err := parseSlurmMemory(value)
		if err != nil {
			return err
		}
		r.MemoryPerTask = q
	case "mem-per-cpu":
		q, err := parseSlurmMemory(value)
		if err != nil {
			return err
		}
		r.MemoryPerCPU = q
	case "mem-per-node":
		q, err := parseSlurmMemory(value)
		if err != nil {
			return err
		}
		r.MemoryPerTask = q

	// ── Resources: GPU / GRES
	case "gres":
		if strings.HasPrefix(value, "gpu") {
			gpu, err := parseGRES(value)
			if err != nil {
				return err
			}
			r.GPU = gpu
		} else {
			// Non-GPU GRES: store in extensions
			setSlurm(job, "gres_"+sanitizeKey(value), value)
		}
	case "gpus", "G":
		// --gpus=type:count or --gpus=count
		gpu, err := parseGRES("gpu:" + value)
		if err != nil {
			return err
		}
		r.GPU = gpu
	case "gpus-per-node", "gpus-per-task":
		gpu, err := parseGRES("gpu:" + value)
		if err != nil {
			return err
		}
		r.GPU = gpu

	// ── Placement
	case "constraint", "C":
		p.Constraint = value
	case "nodelist", "w":
		setSlurm(job, "nodelist", value)
	case "exclude", "x":
		setSlurm(job, "exclude", value)

	// ── Output
	case "output", "o":
		job.Spec.Output.Stdout = normalizePath(value)
	case "error", "e":
		job.Spec.Output.Stderr = normalizePath(value)
	case "open-mode":
		switch value {
		case "append":
			job.Spec.Output.OpenMode = splat.OutputAppend
		case "truncate":
			job.Spec.Output.OpenMode = splat.OutputTruncate
		}

	// ── Notifications
	case "mail-user":
		ensureNotifications(job).Email = value
	case "mail-type":
		for _, ev := range strings.Split(value, ",") {
			mapMailEvent(job, strings.TrimSpace(ev))
		}

	// ── Signal / checkpoint
	case "signal":
		job.Spec.Schedule.SignalBeforeEnd = value
		setSlurm(job, "signal_before_end", value)

	// ── Array
	case "array", "a":
		arr, err := parseArray(value)
		if err != nil {
			return err
		}
		job.Spec.Array = arr

	// ── Dependencies
	case "dependency", "d":
		deps, err := parseDependency(value)
		if err != nil {
			return err
		}
		job.Spec.Dependencies = append(job.Spec.Dependencies, deps...)

	// ── Job lifecycle
	case "kill-on-invalid-dep":
		setSlurm(job, "kill_on_invalid_dep", value)
	case "max-submit-wait":
		setSlurm(job, "max_submit_wait", value)

	// ── Burst buffer / licenses / special
	case "bb", "burst-buffer":
		setSlurm(job, "burst_buffer", value)
	case "bbf":
		setSlurm(job, "burst_buffer_file", value)
	case "licenses", "L":
		setSlurm(job, "licenses", value)
	case "clusters", "cluster", "M":
		setSlurm(job, "clusters", value)
	case "comment":
		if job.Metadata.Annotations == nil {
			job.Metadata.Annotations = map[string]string{}
		}
		job.Metadata.Annotations["bammm.io/comment"] = value
	case "wckey":
		setSlurm(job, "wckey", value)
	case "profile":
		setSlurm(job, "profile", value)

	// ── Het-job marker
	case "hetjob":
		setSlurm(job, "het_job", "true")

	// ── Silently ignore directives we know about but don't translate
	case "chdir", "D", "export", "export-file", "get-user-env",
		"input", "i", "no-kill", "k", "parsable", "quiet", "Q",
		"verbose", "v", "version", "V", "wrap":
		// not mapped to SPLAT

	default:
		// Unknown directives land in extensions for round-trip fidelity
		setSlurm(job, sanitizeKey(flag), value)
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func ensureNotifications(job *splat.Job) *splat.Notifications {
	if job.Spec.Notifications == nil {
		job.Spec.Notifications = &splat.Notifications{}
	}
	return job.Spec.Notifications
}

func setSlurm(job *splat.Job, key, value string) {
	if job.Spec.Extensions.Slurm == nil {
		job.Spec.Extensions.Slurm = map[string]interface{}{}
	}
	job.Spec.Extensions.Slurm[key] = value
}

func isShellExpr(s string) bool {
	return strings.ContainsAny(s, "${}") || strings.Contains(s, "((") ||
		(strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">"))
}

func sanitizeKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "-", "_")
}

// normalizePath replaces Slurm's %j token with the SPLAT template variable.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "%j", "{job_id}")
	p = strings.ReplaceAll(p, "%x", "{job_name}")
	p = strings.ReplaceAll(p, "%a", "{array_index}")
	p = strings.ReplaceAll(p, "%A", "{array_job_id}")
	p = strings.ReplaceAll(p, "%N", "{node_name}")
	p = strings.ReplaceAll(p, "%n", "{node_rank}")
	p = strings.ReplaceAll(p, "%t", "{task_rank}")
	return p
}

// parseDuration wraps splat.DurationOf for string input.
func parseDuration(s string) (*splat.Duration, error) {
	var d splat.Duration
	b := []byte(`"` + s + `"`)
	if err := d.UnmarshalJSON(b); err != nil {
		return nil, fmt.Errorf("duration %q: %w", s, err)
	}
	return &d, nil
}

// parseSlurmMemory parses Slurm memory strings. Slurm treats K/M/G/T as binary
// (KiB/MiB/GiB/TiB). Two-char variants GB/MB/etc. are also accepted (same meaning).
// A bare integer is in MiB.
func parseSlurmMemory(s string) (*splat.Quantity, error) {
	s = strings.TrimSpace(s)
	// Shell variable or expression: cannot resolve at parse time.
	if strings.ContainsAny(s, "${}") {
		return nil, fmt.Errorf("memory %q: contains shell variable (cannot resolve at parse time)", s)
	}
	upper := strings.ToUpper(s)
	// Two-char suffixes first (GB, MB, KB, TB), then single-char.
	for _, pair := range [][2]string{{"TB", "Ti"}, {"GB", "Gi"}, {"MB", "Mi"}, {"KB", "Ki"}, {"T", "Ti"}, {"G", "Gi"}, {"M", "Mi"}, {"K", "Ki"}} {
		if strings.HasSuffix(upper, pair[0]) {
			num := s[:len(s)-len(pair[0])]
			iec := num + pair[1]
			var q splat.Quantity
			if err := q.UnmarshalJSON([]byte(`"` + iec + `"`)); err != nil {
				return nil, fmt.Errorf("memory %q: %w", s, err)
			}
			return &q, nil
		}
	}
	// Bare integer: MiB
	var q splat.Quantity
	if err := q.UnmarshalJSON([]byte(s)); err != nil {
		return nil, fmt.Errorf("memory %q: %w", s, err)
	}
	return &q, nil
}

// parseGRES parses --gres=gpu[:type][:count] into a GPURequest.
func parseGRES(s string) (*splat.GPURequest, error) {
	parts := strings.Split(s, ":")
	gpu := &splat.GPURequest{}
	switch len(parts) {
	case 1: // "gpu"
		gpu.Count = 1
	case 2: // "gpu:4" or "gpu:a100"
		n, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			// "gpu:a100" — type only, assume count 1
			gpu.Type = parts[1]
			gpu.Count = 1
		} else {
			gpu.Count = n
		}
	case 3: // "gpu:a100:2"
		gpu.Type = parts[1]
		n, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return nil, fmt.Errorf("gres count %q: %w", parts[2], err)
		}
		gpu.Count = n
	default:
		return nil, fmt.Errorf("unrecognized gres format %q", s)
	}
	return gpu, nil
}

// parseIntOrRange parses "4" or "2-4" (taking the minimum).
func parseIntOrRange(s string) (int, error) {
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("integer/range %q: %w", s, err)
	}
	return n, nil
}

// parseArray parses --array=0-99, 0-99%10, 1,3,5, etc.
func parseArray(s string) (*splat.Array, error) {
	a := &splat.Array{}
	if idx := strings.IndexByte(s, '%'); idx >= 0 {
		n, err := strconv.Atoi(s[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("array max-concurrent %q: %w", s, err)
		}
		a.MaxConcurrent = n
		s = s[:idx]
	}
	a.Indices = s
	return a, nil
}

// parseDependency parses --dependency=afterok:12345:67890,afterany:111.
func parseDependency(s string) ([]splat.Dependency, error) {
	var deps []splat.Dependency
	for _, clause := range strings.Split(s, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		parts := strings.SplitN(clause, ":", 2)
		scheme := splat.DependencyScheme(parts[0])
		if len(parts) == 1 {
			deps = append(deps, splat.Dependency{Scheme: scheme})
			continue
		}
		// Multiple job IDs: afterok:111:222:333
		for _, id := range strings.Split(parts[1], ":") {
			id = strings.TrimSpace(id)
			if id != "" {
				deps = append(deps, splat.Dependency{Scheme: scheme, Value: id})
			}
		}
	}
	return deps, nil
}

func mapMailEvent(job *splat.Job, event string) {
	n := ensureNotifications(job)
	switch strings.ToUpper(event) {
	case "BEGIN":
		n.Events = append(n.Events, splat.NotifyBegin)
	case "END":
		n.Events = append(n.Events, splat.NotifyEnd)
	case "FAIL":
		n.Events = append(n.Events, splat.NotifyFail)
	case "REQUEUE":
		n.Events = append(n.Events, splat.NotifyRequeue)
	case "TIME_LIMIT_50":
		n.Events = append(n.Events, splat.NotifyTimeLimit50)
	case "TIME_LIMIT_80":
		n.Events = append(n.Events, splat.NotifyTimeLimit80)
	case "TIME_LIMIT_90":
		n.Events = append(n.Events, splat.NotifyTimeLimit90)
	case "ALL":
		n.Events = append(n.Events, splat.NotifyBegin, splat.NotifyEnd, splat.NotifyFail)
	case "NONE", "INVALID_DEPEND":
		// no-op
	}
}
