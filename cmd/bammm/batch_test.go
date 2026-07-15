package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSlurm = "#!/bin/bash\n#SBATCH --job-name=x\n#SBATCH --ntasks=1\necho hi\n"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s: %v", path, err)
	}
}

func TestSwapExt(t *testing.T) {
	cases := map[string]string{
		"foo.sh":       "foo.yaml",
		"a/b/c.sbatch": "a/b/c.yaml",
		"noext":        "noext.yaml",
	}
	for in, want := range cases {
		if got := swapExt(in, ".yaml"); got != want {
			t.Errorf("swapExt(%q): got %q want %q", in, got, want)
		}
	}
}

func TestGatherInputs_Modes(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.sh")
	f2 := filepath.Join(dir, "b.sh")
	writeFile(t, f1, validSlurm)
	writeFile(t, f2, validSlurm)

	// Single file → not batch.
	items, batch, err := gatherInputs([]string{f1}, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if batch || len(items) != 1 {
		t.Errorf("single arg: batch=%v items=%d, want false/1", batch, len(items))
	}

	// Two files → batch.
	_, batch, err = gatherInputs([]string{f1, f2}, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !batch {
		t.Error("two args: expected batch mode")
	}

	// --input-dir → batch even with one file inside.
	_, batch, err = gatherInputs(nil, dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !batch {
		t.Error("--input-dir: expected batch mode")
	}

	// Directory as a positional arg → error (must use --input-dir).
	if _, _, err := gatherInputs([]string{dir}, "", "", true); err == nil {
		t.Error("expected error for directory positional arg")
	}
}

func TestWalkInputDir_PatternAndRecursion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.sh"), validSlurm)
	writeFile(t, filepath.Join(dir, "note.txt"), "ignore me")
	writeFile(t, filepath.Join(dir, "sub", "b.sh"), validSlurm)

	// Pattern filters by filename; recursive picks up the subdirectory.
	items, err := walkInputDir(dir, "*.sh", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("recursive *.sh: got %d items, want 2", len(items))
	}

	// Non-recursive stays in the top directory.
	items, err = walkInputDir(dir, "*.sh", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("non-recursive *.sh: got %d items, want 1", len(items))
	}
}

func TestRunBatch_ContinuesAndMirrors(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeFile(t, filepath.Join(in, "good1.sh"), validSlurm)
	writeFile(t, filepath.Join(in, "sub", "good2.sbatch"), validSlurm)
	writeFile(t, filepath.Join(in, "bad.sh"), "not a slurm script\n")

	items, _, err := gatherInputs(nil, in, "", true)
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	res, err := runBatch(items, "slurm", "splat", out, &buf)
	if err != nil {
		t.Fatalf("runBatch: %v", err)
	}
	if res.converted != 2 {
		t.Errorf("converted=%d want 2", res.converted)
	}
	if len(res.failures) != 1 {
		t.Fatalf("failures=%d want 1", len(res.failures))
	}
	if res.failures[0].rel != "bad.sh" {
		t.Errorf("failed file: got %q want bad.sh", res.failures[0].rel)
	}

	// Tree mirrored, extensions swapped to the target's.
	mustExist(t, filepath.Join(out, "good1.yaml"))
	mustExist(t, filepath.Join(out, "sub", "good2.yaml"))
	if _, err := os.Stat(filepath.Join(out, "bad.yaml")); err == nil {
		t.Error("failed input should not produce an output file")
	}

	// Summary mentions both the count and the failure.
	summary := buf.String()
	if !strings.Contains(summary, "converted 2") || !strings.Contains(summary, "bad.sh") {
		t.Errorf("summary missing detail:\n%s", summary)
	}
}
