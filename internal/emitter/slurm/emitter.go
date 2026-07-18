// Package slurm emits Slurm batch scripts (#SBATCH directives) from SPLAT jobs.
// It is the inverse of internal/parser/slurm.
package slurm

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() {
	emitter.Register("slurm", emitterImpl{})
}

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

// extKeysHandledStructurally lists extensions.slurm keys that are already
// represented by a first-class SPLAT field, so we must not emit them twice.
var extKeysHandledStructurally = map[string]bool{
	"time_min":          true, // -> schedule.walltimeMin
	"signal_before_end": true, // -> schedule.signalBeforeEnd
}

// extKeyToFlag maps sanitized extension keys back to their Slurm long flag
// where the flag name differs from a plain underscore→dash substitution.
var extKeyToFlag = map[string]string{
	"burst_buffer":        "bb",
	"burst_buffer_file":   "bbf",
	"kill_on_invalid_dep": "kill-on-invalid-dep",
	"max_submit_wait":     "max-submit-wait",
}

// Emit converts a SPLAT job into a Slurm batch script. Multi-role jobs
// (spec.tasks) are emitted as a Slurm het-job; single-role jobs use a flat
// allocation.
func Emit(job *splat.Job) ([]byte, error) {
	if len(job.Spec.Tasks) > 0 {
		return emitHetJob(job)
	}

	var b strings.Builder
	writeShebang(&b, job)

	for _, line := range directives(job) {
		fmt.Fprintf(&b, "#SBATCH %s\n", line)
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

// writeShebang writes the interpreter line, honoring execution.shell.
func writeShebang(b *strings.Builder, job *splat.Job) {
	shell := job.Spec.Execution.Shell
	if shell == "" {
		shell = "/bin/bash"
	}
	if strings.HasPrefix(shell, "/") {
		fmt.Fprintf(b, "#!%s\n", shell)
	} else {
		fmt.Fprintf(b, "#!/usr/bin/env %s\n", shell)
	}
}

// directives returns the ordered list of #SBATCH argument strings (without the
// "#SBATCH " prefix).
func directives(job *splat.Job) []string {
	var out []string
	add := func(flag, value string) {
		if value == "" {
			out = append(out, "--"+flag)
			return
		}
		out = append(out, fmt.Sprintf("--%s=%s", flag, value))
	}

	s := job.Spec.Schedule
	r := job.Spec.Resources
	p := job.Spec.Placement

	// ── Identity
	if job.Metadata.Name != "" {
		add("job-name", job.Metadata.Name)
	}

	// ── Queue / scheduling
	queue := s.Partition
	if queue == "" {
		queue = s.Queue
	}
	if queue != "" {
		add("partition", queue)
	}
	if s.Account != "" {
		add("account", s.Account)
	}
	if s.QOS != "" {
		add("qos", s.QOS)
	}
	if s.Priority != 0 {
		add("priority", strconv.Itoa(s.Priority))
	}
	if s.Reservation != "" {
		add("reservation", s.Reservation)
	}
	if s.Hold {
		add("hold", "")
	}
	if s.Exclusive {
		if s.ExclusiveMode != "" {
			add("exclusive", s.ExclusiveMode)
		} else {
			add("exclusive", "")
		}
	}
	if s.Oversubscribe {
		add("oversubscribe", "")
	}
	if job.Spec.Lifecycle.RequeueOnFailure {
		add("requeue", "")
	}

	// ── Timing
	if s.Walltime != nil {
		add("time", s.Walltime.String())
	}
	if s.WalltimeMin != nil {
		add("time-min", s.WalltimeMin.String())
	}

	// ── Resources
	if r.Nodes != 0 {
		add("nodes", strconv.Itoa(r.Nodes))
	}
	if r.Tasks != 0 {
		add("ntasks", strconv.Itoa(r.Tasks))
	}
	if r.TasksPerNode != 0 {
		add("ntasks-per-node", strconv.Itoa(r.TasksPerNode))
	}
	if r.TasksPerSocket != 0 {
		add("ntasks-per-socket", strconv.Itoa(r.TasksPerSocket))
	}
	if r.TasksPerCore != 0 {
		add("ntasks-per-core", strconv.Itoa(r.TasksPerCore))
	}
	if r.CPUsPerTask != 0 {
		add("cpus-per-task", strconv.Itoa(r.CPUsPerTask))
	}
	if r.MemoryPerTask != nil {
		add("mem", slurmMem(r.MemoryPerTask))
	}
	if r.MemoryPerCPU != nil {
		add("mem-per-cpu", slurmMem(r.MemoryPerCPU))
	}
	if r.GPU != nil {
		add("gres", slurmGRES(r.GPU))
	}

	// ── Placement
	if p.Constraint != "" {
		add("constraint", p.Constraint)
	}

	// ── Output
	if job.Spec.Output.Stdout != "" {
		add("output", denormalizePath(job.Spec.Output.Stdout))
	}
	if job.Spec.Output.Stderr != "" {
		add("error", denormalizePath(job.Spec.Output.Stderr))
	}
	switch job.Spec.Output.OpenMode {
	case splat.OutputAppend:
		add("open-mode", "append")
	case splat.OutputTruncate:
		add("open-mode", "truncate")
	}

	// ── Array
	if a := job.Spec.Array; a != nil && a.Indices != "" {
		val := a.Indices
		if a.MaxConcurrent > 0 {
			val = fmt.Sprintf("%s%%%d", val, a.MaxConcurrent)
		}
		add("array", val)
	}

	// ── Dependencies
	if dep := slurmDependency(job.Spec.Dependencies); dep != "" {
		add("dependency", dep)
	}

	// ── Notifications
	if n := job.Spec.Notifications; n != nil {
		if types := mailTypes(n.Events); types != "" {
			add("mail-type", types)
		}
		if n.Email != "" {
			add("mail-user", n.Email)
		}
	}

	// ── Signal
	if s.SignalBeforeEnd != "" {
		add("signal", s.SignalBeforeEnd)
	}

	// ── Comment (round-trips through metadata annotation)
	if c := job.Metadata.Annotations["bammm.io/comment"]; c != "" {
		add("comment", c)
	}

	// ── Working directory
	if wd := job.Spec.Execution.WorkingDir; wd != "" {
		add("chdir", wd)
	}

	// ── Extensions passthrough (sorted for determinism)
	out = append(out, extensionDirectives(job.Spec.Extensions.Slurm)...)

	return out
}

// extensionDirectives renders any scheduler-specific passthrough entries that
// were not already emitted from structured fields.
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
		if extKeysHandledStructurally[k] {
			continue
		}
		val, ok := scalarString(ext[k])
		if !ok {
			// Non-scalar passthrough (nested maps/lists) has no #SBATCH form.
			continue
		}
		switch {
		case k == "het_job":
			out = append(out, "hetjob")
		case strings.HasPrefix(k, "gres_"):
			out = append(out, "--gres="+val)
		default:
			flag := k
			if mapped, ok := extKeyToFlag[k]; ok {
				flag = mapped
			} else {
				flag = strings.ReplaceAll(k, "_", "-")
			}
			if val == "" {
				out = append(out, "--"+flag)
			} else {
				out = append(out, fmt.Sprintf("--%s=%s", flag, val))
			}
		}
	}
	return out
}

// scalarString renders scalar extension values (string/number/bool) to a
// string. It returns ok=false for composite values that have no directive form.
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

// bodyFor returns the script body to place beneath the directive header.
func bodyFor(job *splat.Job) string {
	e := job.Spec.Execution
	if e.Script != "" {
		// The emitter writes its own shebang; drop a leading one from the body
		// so round-tripped scripts don't carry two.
		return stripLeadingShebang(e.Script)
	}
	if e.Executable != "" {
		cmd := "srun " + e.Executable
		if e.Arguments != "" {
			cmd += " " + e.Arguments
		}
		return cmd
	}
	if e.Container != nil && e.Container.Image != "" {
		// No bare-metal script available; leave a Singularity placeholder so the
		// output is runnable-after-edit rather than silently empty.
		return fmt.Sprintf("srun singularity exec %s %s",
			"docker://"+strings.TrimPrefix(e.Container.Image, "docker://"),
			strings.Join(e.Container.Command, " "))
	}
	return ""
}

// stripLeadingShebang removes a leading "#!..." interpreter line (and the blank
// line following it, if any) from a script body.
func stripLeadingShebang(body string) string {
	if !strings.HasPrefix(body, "#!") {
		return body
	}
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		return strings.TrimLeft(body[nl+1:], "\n")
	}
	return ""
}

// slurmMem renders a Quantity as a Slurm memory string (K/M/G/T = binary).
// The SPLAT Quantity stores canonical bytes and prints IEC ("8Gi"); Slurm uses
// the same binary multipliers but spells them without the trailing "i".
func slurmMem(q *splat.Quantity) string {
	iec := q.String()
	if strings.HasSuffix(iec, "i") {
		return iec[:len(iec)-1]
	}
	// Bare byte count (not a clean multiple): express as MB, rounding up.
	mb := (q.Bytes() + (1 << 20) - 1) / (1 << 20)
	return strconv.FormatInt(mb, 10) + "M"
}

// slurmGRES renders a GPURequest as a --gres value: gpu[:type]:count.
func slurmGRES(g *splat.GPURequest) string {
	count := g.Count
	if count == 0 {
		count = 1
	}
	countStr := strconv.FormatInt(int64(math.Round(count)), 10)
	if g.Type != "" {
		return fmt.Sprintf("gpu:%s:%s", g.Type, countStr)
	}
	return "gpu:" + countStr
}

// slurmDependency groups dependencies by scheme and renders the --dependency value.
// Parser splits "afterok:1:2" into separate entries with the same scheme; this
// regroups them into "afterok:1:2".
func slurmDependency(deps []splat.Dependency) string {
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

// mailTypes renders SPLAT notification events as a Slurm --mail-type value.
func mailTypes(events []splat.NotificationEvent) string {
	m := map[splat.NotificationEvent]string{
		splat.NotifyBegin:       "BEGIN",
		splat.NotifyEnd:         "END",
		splat.NotifyFail:        "FAIL",
		splat.NotifyRequeue:     "REQUEUE",
		splat.NotifyTimeLimit50: "TIME_LIMIT_50",
		splat.NotifyTimeLimit80: "TIME_LIMIT_80",
		splat.NotifyTimeLimit90: "TIME_LIMIT_90",
	}
	var parts []string
	for _, e := range events {
		if s, ok := m[e]; ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

// denormalizePath is the inverse of the parser's normalizePath: it restores
// Slurm filename tokens from SPLAT template variables.
func denormalizePath(p string) string {
	replacements := []struct{ from, to string }{
		{"{job_id}", "%j"},
		{"{job_name}", "%x"},
		{"{array_index}", "%a"},
		{"{array_job_id}", "%A"},
		{"{node_name}", "%N"},
		{"{node_rank}", "%n"},
		{"{task_rank}", "%t"},
	}
	for _, r := range replacements {
		p = strings.ReplaceAll(p, r.from, r.to)
	}
	return p
}
