package main

import (
	"path/filepath"
	"testing"
)

func TestBuildReport(t *testing.T) {
	dir := t.TempDir()
	// Rich job: partition, walltime, gpu.
	writeFile(t, filepath.Join(dir, "rich.sh"),
		"#!/bin/bash\n#SBATCH --job-name=rich\n#SBATCH --partition=gpu\n#SBATCH --time=01:00:00\n#SBATCH --gres=gpu:1\necho hi\n")
	// Minimal job: just a task count.
	writeFile(t, filepath.Join(dir, "min.sh"),
		"#!/bin/bash\n#SBATCH --job-name=min\n#SBATCH --ntasks=1\necho hi\n")
	// Unparseable.
	writeFile(t, filepath.Join(dir, "bad.sh"), "not a slurm script\n")

	items, _, err := gatherInputs(nil, dir, "*.sh", true)
	if err != nil {
		t.Fatal(err)
	}

	rep := buildReport(items, "slurm")

	if rep.parsed != 2 {
		t.Errorf("parsed=%d want 2", rep.parsed)
	}
	if rep.parseErrors != 1 {
		t.Errorf("parseErrors=%d want 1", rep.parseErrors)
	}
	if rep.featureHits["walltime"] != 1 {
		t.Errorf("walltime hits=%d want 1", rep.featureHits["walltime"])
	}
	if rep.featureHits["gpu"] != 1 {
		t.Errorf("gpu hits=%d want 1", rep.featureHits["gpu"])
	}
	if rep.featureHits["queue/partition"] != 1 {
		t.Errorf("queue/partition hits=%d want 1", rep.featureHits["queue/partition"])
	}
	if rep.featureHits["script exec"] != 2 {
		t.Errorf("script exec hits=%d want 2", rep.featureHits["script exec"])
	}
}

func TestBar(t *testing.T) {
	if got := bar(0, 10); got != "...................." {
		t.Errorf("bar(0,10)=%q", got)
	}
	if got := bar(10, 10); got != "####################" {
		t.Errorf("bar(10,10)=%q", got)
	}
	if got := bar(5, 10); got != "##########.........." {
		t.Errorf("bar(5,10)=%q", got)
	}
	if got := bar(1, 0); got != "" {
		t.Errorf("bar(1,0)=%q want empty", got)
	}
}
