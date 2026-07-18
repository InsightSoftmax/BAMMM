// Package htcondor emits HTCondor submit description files (.sub) from SPLAT
// jobs. It is the inverse of internal/parser/htcondor.
package htcondor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("htcondor", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// Emit converts a SPLAT job into an HTCondor submit file.
func Emit(job *splat.Job) ([]byte, error) {
	var b strings.Builder
	e := job.Spec.Execution
	r := job.Spec.Resources
	ext := job.Spec.Extensions.HTCondor

	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, v)
		}
	}

	// universe: honor the source, else infer from execution.
	if u := extString(ext, "universe"); u != "" {
		line("universe", u)
	} else if e.Container != nil && e.Container.Image != "" {
		line("universe", "docker")
	} else {
		line("universe", "vanilla")
	}
	if e.Container != nil && e.Container.Image != "" {
		line("docker_image", e.Container.Image)
	}

	if job.Metadata.Name != "" {
		line("batch_name", job.Metadata.Name)
	}
	if e.Executable != "" {
		line("executable", e.Executable)
	} else if e.Container != nil && len(e.Container.Command) > 0 {
		line("executable", e.Container.Command[0])
	}
	if e.Arguments != "" {
		line("arguments", denormalizeVars(e.Arguments))
	} else if e.Container != nil && len(e.Container.Args) > 0 {
		line("arguments", denormalizeVars(strings.Join(e.Container.Args, " ")))
	}

	if r.CPUsPerTask > 0 {
		line("request_cpus", strconv.Itoa(r.CPUsPerTask))
	}
	if r.MemoryPerTask != nil {
		line("request_memory", condorSize(r.MemoryPerTask))
	}
	if r.DiskPerTask != nil {
		line("request_disk", condorSize(r.DiskPerTask))
	}
	if r.GPU != nil && r.GPU.Count > 0 {
		line("request_gpus", strconv.Itoa(int(r.GPU.Count+0.5)))
	}

	if e.Stdin != "" {
		line("input", e.Stdin)
	}
	if job.Spec.Output.Stdout != "" {
		line("output", denormalizeVars(job.Spec.Output.Stdout))
	}
	if job.Spec.Output.Stderr != "" {
		line("error", denormalizeVars(job.Spec.Output.Stderr))
	}
	if job.Spec.Schedule.Priority != 0 {
		line("priority", strconv.Itoa(splat.HTCondorPriority.Denormalize(job.Spec.Schedule.Priority)))
	}
	if env := envString(e.Environment.Vars); env != "" {
		line("environment", `"`+env+`"`)
	}
	if e.Environment.InheritFromSubmitter {
		line("getenv", "true")
	}

	if fs := job.Spec.FileStaging; fs != nil {
		if in := transferList(fs.Inputs); in != "" {
			line("transfer_input_files", in)
		}
		if out := transferList(fs.Outputs); out != "" {
			line("transfer_output_files", out)
		}
	}
	if job.Spec.Lifecycle.MaxRetries > 0 {
		line("max_retries", strconv.Itoa(job.Spec.Lifecycle.MaxRetries))
	}
	if n := job.Spec.Notifications; n != nil {
		if ev := notification(n.Events); ev != "" {
			line("notification", ev)
		}
		if n.Email != "" {
			line("notify_user", n.Email)
		}
	}

	// Accounting attributes.
	if p := job.Spec.Schedule.Project; p != "" {
		line("+ProjectName", p)
	}
	if a := job.Spec.Schedule.Account; a != "" {
		line("+AccountingGroup", a)
	}

	// ClassAd expressions and any other preserved submit commands.
	writeExtensions(ext, line)

	// The queue statement must come last.
	b.WriteString(queueStatement(job))
	return []byte(b.String()), nil
}

// writeExtensions emits preserved submit commands (requirements, rank,
// periodic_*, when_to_transfer_output, …) in a stable order, skipping the ones
// already emitted from structured fields.
func writeExtensions(ext map[string]interface{}, line func(k, v string)) {
	if len(ext) == 0 {
		return
	}
	skip := map[string]bool{
		"universe": true, "queue": true,
		"project_name": true, "accounting_group": true,
	}
	keys := make([]string, 0, len(ext))
	for k := range ext {
		if !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := scalarString(ext[k])
		if !ok {
			continue
		}
		line(k, v)
	}
}

// queueStatement rebuilds the trailing queue command.
func queueStatement(job *splat.Job) string {
	if q := extString(job.Spec.Extensions.HTCondor, "queue"); q != "" {
		return "queue " + q + "\n"
	}
	if a := job.Spec.Array; a != nil && a.Indices != "" {
		if n := indexCount(a.Indices); n > 0 {
			return fmt.Sprintf("queue %d\n", n)
		}
	}
	return "queue\n"
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func extString(ext map[string]interface{}, key string) string {
	if ext == nil {
		return ""
	}
	if v, ok := scalarString(ext[key]); ok {
		return v
	}
	return ""
}

// condorSize renders a Quantity as an HTCondor size (K/M/G/T = powers of 1024).
func condorSize(q *splat.Quantity) string {
	iec := q.String() // e.g. "32Gi"
	if strings.HasSuffix(iec, "i") {
		return iec[:len(iec)-1] // "32Gi" -> "32G"
	}
	return iec
}

func envString(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+vars[k])
	}
	return strings.Join(parts, " ")
}

func transferList(ts []splat.FileTransfer) string {
	if len(ts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, denormalizeVars(t.Src))
	}
	return strings.Join(parts, ", ")
}

func notification(events []splat.NotificationEvent) string {
	var begin, end, fail bool
	for _, e := range events {
		switch e {
		case splat.NotifyBegin:
			begin = true
		case splat.NotifyEnd:
			end = true
		case splat.NotifyFail:
			fail = true
		}
	}
	switch {
	case begin && end && fail:
		return "Always"
	case fail && !end:
		return "Error"
	case end:
		return "Complete"
	default:
		return ""
	}
}

// denormalizeVars is the inverse of the parser's normalizeVars.
func denormalizeVars(s string) string {
	s = strings.ReplaceAll(s, "{job_id}", "$(Cluster)")
	s = strings.ReplaceAll(s, "{array_index}", "$(Process)")
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '{')
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteString("$(" + s[i+1:i+j] + ")")
		s = s[i+j+1:]
	}
	return b.String()
}

// indexCount returns the number of elements in a simple "0-N" index range.
func indexCount(indices string) int {
	lo, hi, ok := strings.Cut(indices, "-")
	if !ok {
		return 0
	}
	l, err1 := strconv.Atoi(strings.TrimSpace(lo))
	h, err2 := strconv.Atoi(strings.TrimSpace(hi))
	if err1 != nil || err2 != nil || h < l {
		return 0
	}
	return h - l + 1
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
