package slurm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

// emitHetJob renders a multi-role SPLAT job as a Slurm het-job (one component
// per task), and appends a commented "flatten to max resources" alternative for
// clusters without het-job support (Slurm < 20.11).
func emitHetJob(job *splat.Job) ([]byte, error) {
	var b strings.Builder
	writeShebang(&b, job)

	// Shared directives come from the top-level schedule/metadata (per-task
	// resources are emitted per component below).
	for _, line := range directives(job) {
		fmt.Fprintf(&b, "#SBATCH %s\n", line)
	}
	// Tag the job with the Armada jobSetId for manual grouping, if present and
	// not already emitted via --comment.
	if jsid := armadaJobSetID(job); jsid != "" && job.Metadata.Annotations["bammm.io/comment"] == "" {
		fmt.Fprintf(&b, "#SBATCH --comment=%s\n", jsid)
		fmt.Fprintf(&b, "#SBATCH --wckey=%s\n", jsid)
	}

	for i, task := range job.Spec.Tasks {
		if i > 0 {
			b.WriteString("#SBATCH hetjob\n")
		}
		name := task.Name
		if name == "" {
			name = fmt.Sprintf("component-%d", i)
		}
		fmt.Fprintf(&b, "# het component %d: %s\n", i, name)
		for _, line := range componentDirectives(task) {
			fmt.Fprintf(&b, "#SBATCH %s\n", line)
		}
	}

	b.WriteString("\n")
	b.WriteString(hetBody(job))

	return []byte(b.String()), nil
}

// componentDirectives renders the per-component resource directives for one task.
func componentDirectives(task splat.Task) []string {
	var out []string
	add := func(flag, value string) {
		out = append(out, fmt.Sprintf("--%s=%s", flag, value))
	}

	nodes := 1
	if task.Resources != nil && task.Resources.Nodes > 0 {
		nodes = task.Resources.Nodes
	}
	add("nodes", strconv.Itoa(nodes))

	ntasks := task.Replicas
	if task.Resources != nil && task.Resources.Tasks > 0 {
		ntasks = task.Resources.Tasks
	}
	if ntasks <= 0 {
		ntasks = 1
	}
	add("ntasks", strconv.Itoa(ntasks))

	if r := task.Resources; r != nil {
		if r.TasksPerNode > 0 {
			add("ntasks-per-node", strconv.Itoa(r.TasksPerNode))
		}
		if r.CPUsPerTask > 0 {
			add("cpus-per-task", strconv.Itoa(r.CPUsPerTask))
		}
		if r.MemoryPerTask != nil {
			add("mem", slurmMem(r.MemoryPerTask))
		}
		if r.GPU != nil {
			add("gres", slurmGRES(r.GPU))
		}
	}
	if task.Placement != nil && task.Placement.Constraint != "" {
		add("constraint", task.Placement.Constraint)
	}
	return out
}

// hetBody generates the execution body: a het-group srun plus a commented
// single-allocation ("flatten") fallback.
func hetBody(job *splat.Job) string {
	var b strings.Builder

	b.WriteString("# ── Het-job execution (Slurm ≥ 20.11) ─────────────────────────────────────────\n")
	parts := make([]string, 0, len(job.Spec.Tasks))
	for i, task := range job.Spec.Tasks {
		parts = append(parts, fmt.Sprintf("--het-group=%d %s", i, taskCommand(task)))
	}
	b.WriteString("srun " + strings.Join(parts, " : \\\n     ") + "\n")

	b.WriteString("\n")
	b.WriteString(flattenNote(job))
	return b.String()
}

// taskCommand renders the command for one task, wrapping containers in
// Singularity (the standard HPC container runtime).
func taskCommand(task splat.Task) string {
	e := task.Execution
	if e == nil {
		return "true # no execution defined"
	}
	if e.Container != nil && e.Container.Image != "" {
		nv := ""
		if task.Resources != nil && task.Resources.GPU != nil {
			nv = "--nv "
		}
		uri := e.Container.Image
		if !strings.Contains(uri, "://") {
			uri = "docker://" + uri
		}
		tokens := append(append([]string{}, e.Container.Command...), e.Container.Args...)
		return strings.TrimSpace(fmt.Sprintf("singularity exec %s%s %s", nv, uri, shellJoin(tokens)))
	}
	if e.Script != "" {
		return "/bin/bash -c " + shellQuote(e.Script)
	}
	if e.Executable != "" {
		return strings.TrimSpace(e.Executable + " " + e.Arguments)
	}
	return "true # no command"
}

// shellJoin quotes each token as needed and joins them with spaces.
func shellJoin(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = shellQuote(t)
	}
	return strings.Join(quoted, " ")
}

// shellQuote returns a POSIX-shell-safe rendering of s, single-quoting it when
// it contains whitespace or shell metacharacters (preserving newlines verbatim).
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$&|;<>()*?#`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// flattenNote emits a commented single-allocation alternative sized to the
// largest task, for clusters without het-job support.
func flattenNote(job *splat.Job) string {
	maxCPU, maxGPU := 0, 0.0
	var maxMem *splat.Quantity
	for _, t := range job.Spec.Tasks {
		if t.Resources == nil {
			continue
		}
		if t.Resources.CPUsPerTask > maxCPU {
			maxCPU = t.Resources.CPUsPerTask
		}
		if t.Resources.GPU != nil && t.Resources.GPU.Count > maxGPU {
			maxGPU = t.Resources.GPU.Count
		}
		if m := t.Resources.MemoryPerTask; m != nil && (maxMem == nil || m.Bytes() > maxMem.Bytes()) {
			maxMem = m
		}
	}

	var b strings.Builder
	b.WriteString("# ── ALTERNATIVE: flatten to a single allocation (any Slurm version) ───────────\n")
	b.WriteString("# If your cluster does not support het-jobs, replace the header above with a\n")
	b.WriteString("# single allocation sized to the largest role and assign roles by $SLURM_PROCID:\n")
	b.WriteString(fmt.Sprintf("#   #SBATCH --nodes=%d\n", len(job.Spec.Tasks)))
	if maxCPU > 0 {
		b.WriteString(fmt.Sprintf("#   #SBATCH --cpus-per-task=%d\n", maxCPU))
	}
	if maxMem != nil {
		b.WriteString(fmt.Sprintf("#   #SBATCH --mem=%s\n", slurmMem(maxMem)))
	}
	if maxGPU > 0 {
		b.WriteString(fmt.Sprintf("#   #SBATCH --gres=gpu:%d\n", int(maxGPU+0.5)))
	}
	b.WriteString("#   # then branch on $SLURM_PROCID to launch driver (rank 0) vs workers.\n")
	return b.String()
}

// armadaJobSetID returns the Armada jobSetId stored in extensions, if any.
func armadaJobSetID(job *splat.Job) string {
	if ext := job.Spec.Extensions.Armada; ext != nil {
		if v, ok := scalarString(ext["job_set_id"]); ok {
			return v
		}
	}
	return ""
}
