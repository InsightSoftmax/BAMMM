package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvert_RulesFlag(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(`
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      equals:
        spec.schedule.qos: debug
    set:
      spec.schedule.queue: debug-queue
    warn: routed debug qos
`), 0o644); err != nil {
		t.Fatal(err)
	}

	in := "#!/bin/bash\n#SBATCH --job-name=t\n#SBATCH --qos=debug\n#SBATCH --ntasks=1\necho hi\n"
	var out, errBuf bytes.Buffer
	cmd := newConvertCmd()
	cmd.SetArgs([]string{"--from", "slurm", "--to", "pbs", "--rules", rulesPath})
	cmd.SetIn(strings.NewReader(in))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if !strings.Contains(out.String(), "#PBS -q debug-queue") {
		t.Errorf("rule did not route the queue:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "routed debug qos") {
		t.Errorf("expected rule warning on stderr, got: %q", errBuf.String())
	}
}

func TestConvert_RulesFileMissing(t *testing.T) {
	cmd := newConvertCmd()
	cmd.SetArgs([]string{"--from", "slurm", "--to", "pbs", "--rules", "/no/such/rules.yaml"})
	cmd.SetIn(strings.NewReader("#SBATCH --ntasks=1\n"))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a missing --rules file")
	}
}
