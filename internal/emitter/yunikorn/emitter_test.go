package yunikorn_test

import (
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"

	ykemit "github.com/InsightSoftmax/BAMMM/internal/emitter/yunikorn"
	"github.com/InsightSoftmax/BAMMM/internal/jobset"
	"github.com/InsightSoftmax/BAMMM/internal/splat"
)

func TestEmit_SingleRole(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "trainer"},
		Spec: splat.Spec{
			Schedule:  splat.Schedule{Queue: "gpu"},
			Resources: splat.Resources{CPUsPerTask: 8, Tasks: 4},
			Execution: splat.Execution{Container: &splat.ContainerExecution{Image: "img:1", Command: []string{"run"}}},
		},
	}
	out, err := ykemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var j batchv1.Job
	if err := yaml.Unmarshal(out, &j); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if j.Labels["yunikorn.apache.org/app-id"] != "trainer" {
		t.Errorf("app-id: got %q", j.Labels["yunikorn.apache.org/app-id"])
	}
	if j.Labels["yunikorn.apache.org/queue"] != "root.gpu" {
		t.Errorf("queue: got %q want root.gpu", j.Labels["yunikorn.apache.org/queue"])
	}
	if j.Spec.Template.Spec.SchedulerName != "yunikorn" {
		t.Errorf("schedulerName: got %q", j.Spec.Template.Spec.SchedulerName)
	}
	if j.Spec.Parallelism == nil || *j.Spec.Parallelism != 4 {
		t.Errorf("parallelism: got %v want 4", j.Spec.Parallelism)
	}
}

func TestEmit_MultiRoleGang(t *testing.T) {
	job := &splat.Job{
		Metadata: splat.Metadata{Name: "sim"},
		Spec: splat.Spec{
			Schedule: splat.Schedule{Queue: "batch"},
			Gang:     &splat.Gang{MinAvailable: 2},
			Tasks: []splat.Task{
				{Name: "driver", Replicas: 1, Resources: &splat.Resources{CPUsPerTask: 4}, Execution: &splat.Execution{Container: &splat.ContainerExecution{Image: "s:1"}}},
				{Name: "worker", Replicas: 3, Resources: &splat.Resources{CPUsPerTask: 8}, Execution: &splat.Execution{Container: &splat.ContainerExecution{Image: "s:1"}}},
			},
		},
	}
	out, err := ykemit.Emit(job)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(string(out), "kind: JobSet") {
		t.Fatalf("expected JobSet:\n%s", out)
	}
	var js jobset.JobSet
	if err := yaml.Unmarshal(out, &js); err != nil {
		t.Fatalf("unmarshal JobSet: %v", err)
	}
	if len(js.Spec.ReplicatedJobs) != 2 {
		t.Fatalf("replicatedJobs: got %d want 2", len(js.Spec.ReplicatedJobs))
	}
	// Every pod must carry the full task-groups list (YuniKorn requirement) and
	// its own group name.
	for _, rj := range js.Spec.ReplicatedJobs {
		ann := rj.Template.Spec.Template.Annotations
		if ann["yunikorn.apache.org/task-group-name"] != rj.Name {
			t.Errorf("%s: task-group-name = %q", rj.Name, ann["yunikorn.apache.org/task-group-name"])
		}
		tg := ann["yunikorn.apache.org/task-groups"]
		if !strings.Contains(tg, `"name":"driver"`) || !strings.Contains(tg, `"name":"worker"`) {
			t.Errorf("%s: task-groups missing roles: %s", rj.Name, tg)
		}
	}
}
