// Package htcondor parses HTCondor submit description files (.sub) into SPLAT
// jobs. ClassAd expressions (requirements, rank, periodic_*) are Turing-complete
// machine-matching logic with no portable equivalent, so they are preserved
// verbatim in extensions.htcondor rather than translated.
package htcondor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/parser"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	parser.Register("htcondor", parserImpl{})
}

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts an HTCondor submit file into a SPLAT job.
func Parse(data []byte) (*splat.Job, error) {
	lines := logicalLines(data)

	job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
	job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "htcondor"}

	sawCommand := false
	for _, line := range lines {
		if kw, rest, ok := queueStatement(line); ok {
			applyQueue(job, kw, rest)
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		sawCommand = true
		apply(job, key, val)
	}
	if !sawCommand {
		return nil, fmt.Errorf("htcondor: no submit commands found")
	}

	finalizeExecution(job)
	return job, nil
}

// logicalLines returns the submit commands, joining backslash-continued lines
// and dropping comments and blanks.
func logicalLines(data []byte) []string {
	var out []string
	var buf strings.Builder
	for _, raw := range strings.Split(string(data), "\n") {
		line := raw
		// Strip trailing comment (unquoted '#'); HTCondor comments start a line,
		// but trailing comments after a value are common in practice.
		if i := strings.IndexByte(line, '#'); i >= 0 && buf.Len() == 0 && strings.TrimSpace(line[:i]) == "" {
			continue // whole-line comment
		}
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, "\\") {
			buf.WriteString(strings.TrimSuffix(trimmed, "\\"))
			continue
		}
		buf.WriteString(line)
		joined := strings.TrimSpace(buf.String())
		buf.Reset()
		if joined != "" {
			out = append(out, joined)
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func apply(job *splat.Job, key, val string) {
	lk := strings.ToLower(key)
	s := &job.Spec.Schedule
	r := &job.Spec.Resources

	switch lk {
	case "batch_name":
		job.Metadata.Name = val
	case "executable":
		job.Spec.Execution.Executable = val
	case "arguments":
		job.Spec.Execution.Arguments = normalizeVars(val)
	case "docker_image", "container_image":
		container(job).Image = val
	case "request_cpus":
		if n, err := strconv.Atoi(val); err == nil {
			r.CPUsPerTask = n
		}
	case "request_memory":
		if q, err := parseCapacity(val, _MiB); err == nil {
			r.MemoryPerTask = q
		}
	case "request_disk":
		if q, err := parseCapacity(val, _KiB); err == nil {
			r.DiskPerTask = q
		}
	case "request_gpus":
		if g, err := strconv.ParseFloat(val, 64); err == nil {
			ensureGPU(job).Count = g
		}
	case "input":
		job.Spec.Execution.Stdin = val
	case "output":
		job.Spec.Output.Stdout = normalizeVars(val)
	case "error":
		job.Spec.Output.Stderr = normalizeVars(val)
	case "priority":
		if n, err := strconv.Atoi(val); err == nil {
			s.Priority = splat.HTCondorPriority.Normalize(n)
		}
	case "notification":
		applyNotification(job, val)
	case "notify_user":
		ensureNotifications(job).Email = val
	case "max_retries":
		if n, err := strconv.Atoi(val); err == nil {
			job.Spec.Lifecycle.MaxRetries = n
		}
	case "environment":
		applyEnvironment(job, val)
	case "getenv":
		if isTrue(val) {
			job.Spec.Execution.Environment.InheritFromSubmitter = true
		}
	case "transfer_input_files":
		for _, f := range splitList(val) {
			addInput(job, f)
		}
	case "transfer_output_files":
		for _, f := range splitList(val) {
			addOutput(job, f)
		}
	case "should_transfer_files", "when_to_transfer_output":
		setExt(job, lk, val)
	case "queue": // handled by queueStatement, but guard against "queue = x"
	default:
		// +ProjectName / +AccountingGroup carry accounting metadata.
		switch strings.ToLower(strings.TrimPrefix(key, "+")) {
		case "projectname":
			s.Project = val
			setExt(job, "project_name", val)
			return
		case "accountinggroup":
			s.Account = val
			setExt(job, "accounting_group", val)
			return
		}
		// Everything else — ClassAd expressions, universe, custom attrs — is
		// preserved verbatim for round-trip; it has no portable SPLAT field.
		setExt(job, sanitizeKey(key), unquote(val))
	}
}

// applyQueue maps the queue statement to an array when a count is present.
func applyQueue(job *splat.Job, keyword, rest string) {
	// Forms: "queue", "queue N", "queue N var", "queue var from file",
	//        "queue N var from file". Extract a leading integer if any.
	_ = keyword
	fields := strings.Fields(rest)
	if len(fields) > 0 {
		if n, err := strconv.Atoi(fields[0]); err == nil && n > 1 {
			job.Spec.Array = &splat.Array{Indices: fmt.Sprintf("0-%d", n-1)}
		}
	}
	if rest != "" {
		setExt(job, "queue", rest)
	}
}

// finalizeExecution reconciles the container universe with the executable: a
// docker/container universe with an image runs the executable inside it.
func finalizeExecution(job *splat.Job) {
	e := &job.Spec.Execution
	if e.Container != nil && e.Container.Image != "" {
		if e.Executable != "" {
			e.Container.Command = []string{e.Executable}
			if e.Arguments != "" {
				e.Container.Args = strings.Fields(e.Arguments)
			}
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func container(job *splat.Job) *splat.ContainerExecution {
	if job.Spec.Execution.Container == nil {
		job.Spec.Execution.Container = &splat.ContainerExecution{}
	}
	return job.Spec.Execution.Container
}

func ensureGPU(job *splat.Job) *splat.GPURequest {
	if job.Spec.Resources.GPU == nil {
		job.Spec.Resources.GPU = &splat.GPURequest{}
	}
	return job.Spec.Resources.GPU
}

func ensureNotifications(job *splat.Job) *splat.Notifications {
	if job.Spec.Notifications == nil {
		job.Spec.Notifications = &splat.Notifications{}
	}
	return job.Spec.Notifications
}

func setExt(job *splat.Job, key, val string) {
	if job.Spec.Extensions.HTCondor == nil {
		job.Spec.Extensions.HTCondor = map[string]interface{}{}
	}
	job.Spec.Extensions.HTCondor[key] = val
}

func applyNotification(job *splat.Job, val string) {
	n := ensureNotifications(job)
	switch strings.ToLower(val) {
	case "always":
		n.Events = append(n.Events, splat.NotifyBegin, splat.NotifyEnd, splat.NotifyFail)
	case "complete":
		n.Events = append(n.Events, splat.NotifyEnd)
	case "error":
		n.Events = append(n.Events, splat.NotifyFail)
	case "never":
	}
}

func applyEnvironment(job *splat.Job, val string) {
	val = unquote(val)
	vars := map[string]string{}
	for _, kv := range strings.Fields(val) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	if len(vars) > 0 {
		job.Spec.Execution.Environment.Vars = vars
	}
}

func addInput(job *splat.Job, f string) {
	fs := ensureStaging(job)
	fs.Inputs = append(fs.Inputs, splat.FileTransfer{Src: f, Dst: f})
}

func addOutput(job *splat.Job, f string) {
	fs := ensureStaging(job)
	fs.Outputs = append(fs.Outputs, splat.FileTransfer{Src: normalizeVars(f), Dst: normalizeVars(f)})
}

func ensureStaging(job *splat.Job) *splat.FileStaging {
	if job.Spec.FileStaging == nil {
		job.Spec.FileStaging = &splat.FileStaging{}
	}
	return job.Spec.FileStaging
}

// queueStatement returns ("queue", rest, true) if the line is a queue command.
func queueStatement(line string) (keyword, rest string, ok bool) {
	f := strings.Fields(line)
	if len(f) > 0 && strings.EqualFold(f[0], "queue") {
		return "queue", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), f[0])), true
	}
	return "", "", false
}

// normalizeVars maps HTCondor $(Cluster)/$(Process) and $(var) to SPLAT tokens.
func normalizeVars(s string) string {
	s = strings.ReplaceAll(s, "$(Cluster)", "{job_id}")
	s = strings.ReplaceAll(s, "$(Process)", "{array_index}")
	// Named macros $(name) → {name}.
	var b strings.Builder
	for {
		i := strings.Index(s, "$(")
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.IndexByte(s[i:], ')')
		if j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteString("{" + s[i+2:i+j] + "}")
		s = s[i+j+1:]
	}
	return b.String()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sanitizeKey(s string) string {
	s = strings.TrimPrefix(s, "+")
	return strings.ReplaceAll(strings.ToLower(s), "-", "_")
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1":
		return true
	}
	return false
}

const (
	_KiB int64 = 1024
	_MiB int64 = 1024 * _KiB
)

// parseCapacity parses an HTCondor size. A bare number uses defaultUnit
// (MiB for memory, KiB for disk); suffixes K/M/G/T (optionally with B) override.
func parseCapacity(s string, defaultUnit int64) (*splat.Quantity, error) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(strings.TrimSuffix(strings.ToLower(s), "b"))
	units := map[byte]string{'k': "Ki", 'm': "Mi", 'g': "Gi", 't': "Ti"}
	if len(lower) > 0 {
		if iec, ok := units[lower[len(lower)-1]]; ok {
			num := lower[:len(lower)-1]
			var q splat.Quantity
			if err := q.UnmarshalJSON([]byte(`"` + num + iec + `"`)); err != nil {
				return nil, fmt.Errorf("capacity %q: %w", s, err)
			}
			return &q, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("capacity %q: %w", s, err)
	}
	return splat.QuantityOf(n * defaultUnit), nil
}
