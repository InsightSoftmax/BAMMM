package pbs_test

import (
	"os"
	"testing"
	"time"

	"github.com/InsightSoftmax/BAMMM/internal/parser/pbs"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func source(t *testing.T) *splat.Job {
	t.Helper()
	data, err := os.ReadFile("../../../conversions/03-htcondor-to-pbs/target.sh")
	if err != nil {
		t.Fatal(err)
	}
	job, err := pbs.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return job
}

func TestParse_Metadata(t *testing.T) {
	job := source(t)
	if job.Metadata.Name != "genomics-sweep-2026-06" {
		t.Errorf("name: got %q", job.Metadata.Name)
	}
	if job.Metadata.Annotations["bammm.io/source-format"] != "pbs" {
		t.Error("missing source-format annotation")
	}
	if job.Spec.Schedule.Account != "bio.variant-calling" {
		t.Errorf("account: got %q", job.Spec.Schedule.Account)
	}
	if job.Spec.Schedule.Walltime == nil || job.Spec.Schedule.Walltime.Duration() != 48*time.Hour {
		t.Errorf("walltime: got %v want 48h", job.Spec.Schedule.Walltime)
	}
}

func TestParse_SelectResources(t *testing.T) {
	job := source(t)
	r := job.Spec.Resources
	if r.Nodes != 1 {
		t.Errorf("nodes: got %d want 1", r.Nodes)
	}
	if r.CPUsPerTask != 8 {
		t.Errorf("cpus: got %d want 8", r.CPUsPerTask)
	}
	if r.MemoryPerTask == nil || r.MemoryPerTask.String() != "128Gi" {
		t.Errorf("mem: got %v want 128Gi", r.MemoryPerTask)
	}
	if r.GPU == nil || r.GPU.Count != 1 {
		t.Errorf("gpu: got %v want 1", r.GPU)
	}
	if r.DiskPerTask == nil || r.DiskPerTask.String() != "500Gi" {
		t.Errorf("scratch: got %v want 500Gi", r.DiskPerTask)
	}
	// Site-specific chunk resource preserved for round-trip.
	if job.Spec.Extensions.PBS["select_ib"] != "true" {
		t.Errorf("select_ib: got %v want true", job.Spec.Extensions.PBS["select_ib"])
	}
}

func TestParse_ArrayAndNotifications(t *testing.T) {
	job := source(t)
	if job.Spec.Array == nil || job.Spec.Array.Indices != "0-199" {
		t.Errorf("array: got %v want 0-199", job.Spec.Array)
	}
	if job.Spec.Output.Stdout != "/scratch/logs/gatk-{job_id}-{array_index}.out" {
		t.Errorf("stdout: got %q", job.Spec.Output.Stdout)
	}
	n := job.Spec.Notifications
	if n == nil || n.Email != "pipeline@genomics.org" {
		t.Fatalf("notifications: got %v", n)
	}
	if len(n.Events) != 1 || n.Events[0] != splat.NotifyEnd {
		t.Errorf("mail events: got %v want [end]", n.Events)
	}
}

func TestParse_Hardening(t *testing.T) {
	// Comma-separated -l list, inline # comment, and single-letter/uppercase
	// memory suffix — all seen in the real corpus.
	script := []byte("#!/bin/bash\n" +
		"#PBS -N h\n" +
		"#PBS -l nodes=1:ppn=40           # request one node\n" +
		"#PBS -l mem=250gb,walltime=18:00:00\n" +
		"#PBS -l mem=50G\n" +
		"echo hi\n")
	job, err := pbs.Parse(script)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := job.Spec.Resources
	if r.TasksPerNode != 40 {
		t.Errorf("ppn (with inline comment): got %d want 40", r.TasksPerNode)
	}
	// The last mem= wins (50G = 50 GiB).
	if r.MemoryPerTask == nil || r.MemoryPerTask.String() != "50Gi" {
		t.Errorf("mem 50G: got %v want 50Gi", r.MemoryPerTask)
	}
	if job.Spec.Schedule.Walltime == nil || job.Spec.Schedule.Walltime.Duration() != 18*time.Hour {
		t.Errorf("comma-list walltime: got %v want 18h", job.Spec.Schedule.Walltime)
	}
}

func TestParse_LegacyNodes(t *testing.T) {
	script := []byte("#!/bin/bash\n#PBS -N legacy\n#PBS -l nodes=4:ppn=8\n#PBS -l walltime=01:00:00\necho hi\n")
	job, err := pbs.Parse(script)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if job.Spec.Resources.Nodes != 4 {
		t.Errorf("nodes: got %d want 4", job.Spec.Resources.Nodes)
	}
	if job.Spec.Resources.TasksPerNode != 8 {
		t.Errorf("ppn->tasksPerNode: got %d want 8", job.Spec.Resources.TasksPerNode)
	}
}
