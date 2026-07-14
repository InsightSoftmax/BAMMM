package slurm_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/InsightSoftmax/BAMMM/internal/parser/slurm"
)

// source01 is the hand-crafted reference script from conversions/01-slurm-to-volcano/source.sh
var source01 = mustRead("../../../conversions/01-slurm-to-volcano/source.sh")

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func TestParse_Source01_Metadata(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Metadata.Name != "bert-finetune" {
		t.Errorf("name: got %q, want %q", job.Metadata.Name, "bert-finetune")
	}
	if job.Metadata.Annotations["bammm.io/source-format"] != "slurm" {
		t.Error("missing source-format annotation")
	}
}

func TestParse_Source01_Schedule(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := job.Spec.Schedule
	if s.Partition != "gpu-hpc" {
		t.Errorf("partition: got %q, want %q", s.Partition, "gpu-hpc")
	}
	if s.Queue != "gpu-hpc" {
		t.Errorf("queue: got %q, want %q", s.Queue, "gpu-hpc")
	}
	if s.Account != "nlp-research" {
		t.Errorf("account: got %q, want %q", s.Account, "nlp-research")
	}
	if s.QOS != "gpu-qos" {
		t.Errorf("qos: got %q, want %q", s.QOS, "gpu-qos")
	}
	if s.Walltime == nil {
		t.Fatal("walltime is nil")
	}
	if s.Walltime.Duration() != 8*time.Hour {
		t.Errorf("walltime: got %v, want 8h", s.Walltime.Duration())
	}
	if s.SignalBeforeEnd != "USR1@120" {
		t.Errorf("signal: got %q, want USR1@120", s.SignalBeforeEnd)
	}
}

func TestParse_Source01_Resources(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := job.Spec.Resources
	if r.Nodes != 4 {
		t.Errorf("nodes: got %d, want 4", r.Nodes)
	}
	if r.TasksPerNode != 8 {
		t.Errorf("tasks_per_node: got %d, want 8", r.TasksPerNode)
	}
	if r.Tasks != 32 {
		t.Errorf("tasks: got %d, want 32 (4 nodes × 8 tasks)", r.Tasks)
	}
	if r.CPUsPerTask != 6 {
		t.Errorf("cpus_per_task: got %d, want 6", r.CPUsPerTask)
	}
	if r.MemoryPerCPU == nil {
		t.Fatal("memory_per_cpu is nil")
	}
	wantMem := int64(8 * 1024 * 1024 * 1024) // 8Gi
	if r.MemoryPerCPU.Bytes() != wantMem {
		t.Errorf("memory_per_cpu: got %d bytes, want %d (8Gi)", r.MemoryPerCPU.Bytes(), wantMem)
	}
}

func TestParse_Source01_GPU(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	gpu := job.Spec.Resources.GPU
	if gpu == nil {
		t.Fatal("gpu is nil")
	}
	if gpu.Count != 2 {
		t.Errorf("gpu.count: got %v, want 2", gpu.Count)
	}
	if gpu.Type != "a100" {
		t.Errorf("gpu.type: got %q, want a100", gpu.Type)
	}
}

func TestParse_Source01_Placement(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Placement.Constraint != "infiniband&avx512" {
		t.Errorf("constraint: got %q, want infiniband&avx512", job.Spec.Placement.Constraint)
	}
}

func TestParse_Source01_Output(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Output.Stdout != "/scratch/logs/bert-{job_id}.out" {
		t.Errorf("stdout: got %q", job.Spec.Output.Stdout)
	}
	if job.Spec.Output.Stderr != "/scratch/logs/bert-{job_id}.err" {
		t.Errorf("stderr: got %q", job.Spec.Output.Stderr)
	}
}

func TestParse_Source01_Notifications(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	n := job.Spec.Notifications
	if n == nil {
		t.Fatal("notifications is nil")
	}
	if n.Email != "researcher@university.edu" {
		t.Errorf("email: got %q", n.Email)
	}
	events := make(map[string]bool)
	for _, e := range n.Events {
		events[string(e)] = true
	}
	if !events["end"] {
		t.Error("missing notification event: end")
	}
	if !events["fail"] {
		t.Error("missing notification event: fail")
	}
}

func TestParse_Source01_Script(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Execution.Script == "" {
		t.Error("script body is empty")
	}
	if !strings.Contains(job.Spec.Execution.Script, "srun") {
		t.Error("script body should contain srun")
	}
	if !strings.Contains(job.Spec.Execution.Script, "module load") {
		t.Error("script body should contain module load")
	}
}

func TestParse_Source01_Extensions(t *testing.T) {
	job, err := slurm.Parse(source01)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ext := job.Spec.Extensions.Slurm
	if ext == nil {
		t.Fatal("extensions.slurm is nil")
	}
	if ext["time_min"] == nil {
		t.Error("extensions.slurm.time_min should be set")
	}
}

// ── Inline directive parsing tests ────────────────────────────────────────────

func TestParse_ArrayJob(t *testing.T) {
	script := sbatchScript(
		"--job-name=sweep",
		"--array=0-99%10",
		"--nodes=1",
		"--ntasks=1",
		"--time=01:00:00",
	)
	job, err := slurm.Parse([]byte(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Array == nil {
		t.Fatal("array is nil")
	}
	if job.Spec.Array.Indices != "0-99" {
		t.Errorf("array.indices: got %q, want 0-99", job.Spec.Array.Indices)
	}
	if job.Spec.Array.MaxConcurrent != 10 {
		t.Errorf("array.max_concurrent: got %d, want 10", job.Spec.Array.MaxConcurrent)
	}
}

func TestParse_MemFlag(t *testing.T) {
	script := sbatchScript(
		"--job-name=job",
		"--mem=32G",
		"--nodes=1",
		"--time=01:00:00",
	)
	job, err := slurm.Parse([]byte(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Resources.MemoryPerTask == nil {
		t.Fatal("memory_per_task is nil")
	}
	want := int64(32 * 1024 * 1024 * 1024) // 32Gi (Slurm G = GiB in HPC context)
	if job.Spec.Resources.MemoryPerTask.Bytes() != want {
		t.Errorf("memory_per_task: got %d, want %d (32Gi)", job.Spec.Resources.MemoryPerTask.Bytes(), want)
	}
}

func TestParse_GresGPUNoType(t *testing.T) {
	script := sbatchScript(
		"--job-name=job",
		"--gres=gpu:4",
		"--nodes=1",
		"--time=01:00:00",
	)
	job, err := slurm.Parse([]byte(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Resources.GPU == nil {
		t.Fatal("gpu is nil")
	}
	if job.Spec.Resources.GPU.Count != 4 {
		t.Errorf("gpu.count: got %v, want 4", job.Spec.Resources.GPU.Count)
	}
	if job.Spec.Resources.GPU.Type != "" {
		t.Errorf("gpu.type: got %q, want empty", job.Spec.Resources.GPU.Type)
	}
}

func TestParse_DependencyAfterOK(t *testing.T) {
	script := sbatchScript(
		"--job-name=step2",
		"--dependency=afterok:12345",
		"--nodes=1",
		"--time=01:00:00",
	)
	job, err := slurm.Parse([]byte(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(job.Spec.Dependencies) != 1 {
		t.Fatalf("dependencies: got %d, want 1", len(job.Spec.Dependencies))
	}
	if job.Spec.Dependencies[0].Scheme != "afterok" {
		t.Errorf("scheme: got %q, want afterok", job.Spec.Dependencies[0].Scheme)
	}
	if job.Spec.Dependencies[0].Value != "12345" {
		t.Errorf("value: got %q, want 12345", job.Spec.Dependencies[0].Value)
	}
}

func TestParse_ShortFlags(t *testing.T) {
	script := sbatchScript(
		"-J myjob",
		"-N 2",
		"-n 16",
		"-c 4",
		"-t 02:00:00",
		"-p gpu",
	)
	job, err := slurm.Parse([]byte(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Metadata.Name != "myjob" {
		t.Errorf("name: got %q, want myjob", job.Metadata.Name)
	}
	if job.Spec.Resources.Nodes != 2 {
		t.Errorf("nodes: got %d, want 2", job.Spec.Resources.Nodes)
	}
	if job.Spec.Resources.Tasks != 16 {
		t.Errorf("tasks: got %d, want 16", job.Spec.Resources.Tasks)
	}
	if job.Spec.Resources.CPUsPerTask != 4 {
		t.Errorf("cpus_per_task: got %d, want 4", job.Spec.Resources.CPUsPerTask)
	}
	if job.Spec.Schedule.Partition != "gpu" {
		t.Errorf("partition: got %q, want gpu", job.Spec.Schedule.Partition)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	_, err := slurm.Parse([]byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParse_NoSBATCH(t *testing.T) {
	_, err := slurm.Parse([]byte("#!/bin/bash\necho hello\n"))
	if err == nil {
		t.Error("expected error when no #SBATCH directives found")
	}
}

// sbatchScript builds a minimal #SBATCH script from directive strings.
func sbatchScript(directives ...string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	for _, d := range directives {
		b.WriteString("#SBATCH ")
		b.WriteString(d)
		b.WriteString("\n")
	}
	b.WriteString("\necho done\n")
	return b.String()
}
