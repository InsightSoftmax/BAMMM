package rules_test

import (
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/rules"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func load(t *testing.T, doc string) *rules.Ruleset {
	t.Helper()
	rs, err := rules.Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rs
}

func TestLoad_BadAPIVersion(t *testing.T) {
	if _, err := rules.Load([]byte("apiVersion: nope\nrules: []\n")); err == nil {
		t.Fatal("expected error for wrong apiVersion")
	}
}

func TestApply_EqualsThenSetWithWarn(t *testing.T) {
	job := &splat.Job{Spec: splat.Spec{Schedule: splat.Schedule{QOS: "debug"}}}
	rs := load(t, `
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      equals:
        spec.schedule.qos: debug
    set:
      spec.schedule.queue: debug-queue
    warn: routed debug qos to debug-queue
`)
	warns, err := rs.Apply(job, "slurm", "pbs")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if job.Spec.Schedule.Queue != "debug-queue" {
		t.Errorf("queue: got %q want debug-queue", job.Spec.Schedule.Queue)
	}
	if len(warns) != 1 {
		t.Errorf("warnings: got %v want 1", warns)
	}
}

func TestApply_FromToGating(t *testing.T) {
	rule := `
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      to: kueue
    set:
      spec.schedule.queue: k8s-queue
`
	// Rule targets kueue; a pbs conversion must not fire it.
	job := &splat.Job{}
	if _, err := load(t, rule).Apply(job, "slurm", "pbs"); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Schedule.Queue != "" {
		t.Errorf("rule should not have fired for to=pbs; queue=%q", job.Spec.Schedule.Queue)
	}
	// Same rule fires for kueue.
	job = &splat.Job{}
	if _, err := load(t, rule).Apply(job, "slurm", "kueue"); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Schedule.Queue != "k8s-queue" {
		t.Errorf("rule should have fired for to=kueue; queue=%q", job.Spec.Schedule.Queue)
	}
}

func TestApply_DefaultOnlyWhenAbsent(t *testing.T) {
	rs := load(t, `
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when: {}
    default:
      spec.schedule.queue: fallback
`)
	// Absent -> filled.
	a := &splat.Job{}
	if _, err := rs.Apply(a, "slurm", "pbs"); err != nil {
		t.Fatal(err)
	}
	if a.Spec.Schedule.Queue != "fallback" {
		t.Errorf("default not applied: %q", a.Spec.Schedule.Queue)
	}
	// Present -> untouched.
	b := &splat.Job{Spec: splat.Spec{Schedule: splat.Schedule{Queue: "mine"}}}
	if _, err := rs.Apply(b, "slurm", "pbs"); err != nil {
		t.Fatal(err)
	}
	if b.Spec.Schedule.Queue != "mine" {
		t.Errorf("default overwrote existing value: %q", b.Spec.Schedule.Queue)
	}
}

func TestApply_RemoveExtensionWithHas(t *testing.T) {
	job := &splat.Job{Spec: splat.Spec{Extensions: splat.Extensions{
		HTCondor: map[string]any{"requirements": "Memory > 8000"},
	}}}
	rs := load(t, `
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      to: pbs
      has: spec.extensions.htcondor.requirements
    remove:
      - spec.extensions.htcondor.requirements
    warn: dropped HTCondor requirements (no PBS equivalent)
`)
	warns, err := rs.Apply(job, "htcondor", "pbs")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := job.Spec.Extensions.HTCondor["requirements"]; ok {
		t.Error("requirements should have been removed")
	}
	if len(warns) != 1 {
		t.Errorf("expected one warning, got %v", warns)
	}
}

func TestApply_Rename(t *testing.T) {
	job := &splat.Job{Spec: splat.Spec{Schedule: splat.Schedule{Account: "proj-x"}}}
	rs := load(t, `
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      has: spec.schedule.account
    rename:
      spec.schedule.account: spec.schedule.project
`)
	if _, err := rs.Apply(job, "slurm", "flux"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if job.Spec.Schedule.Account != "" {
		t.Errorf("account should be cleared, got %q", job.Spec.Schedule.Account)
	}
	if job.Spec.Schedule.Project != "proj-x" {
		t.Errorf("project: got %q want proj-x", job.Spec.Schedule.Project)
	}
}

func TestApply_NilRulesetIsNoop(t *testing.T) {
	var rs *rules.Ruleset
	job := &splat.Job{Spec: splat.Spec{Schedule: splat.Schedule{Queue: "q"}}}
	warns, err := rs.Apply(job, "slurm", "pbs")
	if err != nil || warns != nil {
		t.Fatalf("nil ruleset should be a no-op: %v, %v", warns, err)
	}
	if job.Spec.Schedule.Queue != "q" {
		t.Error("job mutated by nil ruleset")
	}
}
