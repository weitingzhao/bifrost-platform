package unitexec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

func testPlan() *unitplan.Plan {
	key := strings.Repeat("ab", 32)
	return &unitplan.Plan{
		Repo: "bifrost-platform", Commit: strings.Repeat("9e", 20), Tree: strings.Repeat("37", 20),
		Pins: []unitplan.PlanPin{{Repo: "bifrost-ui", Commit: strings.Repeat("c8", 20)}, {Repo: "bifrost-trade-infra", Commit: strings.Repeat("73", 20)}},
		Units: []unitplan.PlannedTask{{
			Unit: "api/internal/agentrt/store", Task: "test", Module: "api", Key: key, Shards: 8,
			Inputs: map[string]any{
				"template": "bifrost-unit-run-db", "db_roles": "agentrt_owner_dev+agent_reader",
				"image":  "docker.io/library/golang:1.25-bookworm@sha256:" + strings.Repeat("3b", 32),
				"script": map[string]any{"path": "scripts/ci/unit-go-test"},
			},
		}},
	}
}

func testJob() Job {
	p := testPlan()
	return Job{Plan: p, Task: p.Units[0], ShardIndex: 3, ShardTotal: 8, Attempt: 1, Seq: 7, Tier: "branch",
		GiteaBase: "http://gitea.cicd.svc.cluster.local:3000/bifrost", Namespace: "cicd",
		NodeSelector: map[string]string{"bifrost.io/host-id": "ubt-k3s-05"}}
}

// What is dispatched is what Receive reads back: the run names its plan, its
// unit and its whole key, and passes only what the platform identity may.
func TestTaskRunForIsWhatReceiveReads(t *testing.T) {
	job := testJob()
	obj, err := TaskRunFor(job)
	if err != nil {
		t.Fatal(err)
	}
	meta, spec := obj["metadata"].(map[string]any), obj["spec"].(map[string]any)
	id := PlanID(job.Plan)
	if meta["name"] != "unit-"+id[:8]+"-7" || meta["namespace"] != "cicd" {
		t.Errorf("name %v in %v", meta["name"], meta["namespace"])
	}
	want := map[string]string{
		"repo-url": "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git", "revision": job.Plan.Commit,
		"extra-clones": "bifrost-trade-infra@" + strings.Repeat("73", 20) + "+bifrost-ui@" + strings.Repeat("c8", 20),
		"workdir":      "bifrost-platform/api/internal/agentrt/store", "command": "/unit/src/bifrost-platform/scripts/ci/unit-go-test",
		"shard-index": "3", "shard-total": "8", "unit": "api/internal/agentrt/store", "task": "test",
		"db-roles": "agentrt_owner_dev+agent_reader", "image": job.Task.Inputs["image"].(string),
	}
	got := map[string]string{}
	for _, p := range spec["params"].([]any) {
		got[p.(map[string]any)["name"].(string)] = p.(map[string]any)["value"].(string)
	}
	if len(got) != len(want) {
		t.Errorf("parameters %v", got)
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
	// The admission policy's own rules, as read from infra's 40-admission.yaml.
	if _, has := spec["serviceAccountName"]; has {
		t.Error("a platform TaskRun names no service account")
	}
	if _, has := spec["workspaces"]; has {
		t.Error("a unit TaskRun passes no workspace")
	}
	pod := spec["podTemplate"].(map[string]any)
	if len(pod) != 1 || pod["nodeSelector"].(map[string]any)["bifrost.io/host-id"] != "ubt-k3s-05" {
		t.Errorf("podTemplate %v", pod)
	}
	if spec["taskRef"].(map[string]any)["name"] != "bifrost-unit-run-db" || len(spec["taskRef"].(map[string]any)) != 1 {
		t.Errorf("taskRef %v", spec["taskRef"])
	}

	// Round trip: give the object the cluster's own fields of a finished run.
	raw, _ := json.Marshal(obj)
	var tr TaskRun
	if err = json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	tr.Metadata.UID = "uid-1"
	tr.Status.Conditions = append(tr.Status.Conditions, struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}{"Succeeded", "False", "TaskRunImagePullFailed", "no such image"})
	row, done, err := Receive(tr)
	if err != nil || !done {
		t.Fatalf("done %v, %v", done, err)
	}
	if row.Plan != id || row.Unit != "api/internal/agentrt/store" || row.InputKey != job.Task.Key || row.ShardIndex != 3 || row.ShardTotal != 8 ||
		row.CommitSHA != job.Plan.Commit || row.Attempt != 1 || row.Class != "infra" {
		t.Errorf("read back %+v", row)
	}
}

func TestTaskRunForNamesAndRefusals(t *testing.T) {
	job := testJob()
	job.Attempt = 2
	obj, _ := TaskRunFor(job)
	if name := obj["metadata"].(map[string]any)["name"].(string); !strings.HasSuffix(name, "-7-r2") {
		t.Errorf("the retry is named %s", name)
	}
	again := testPlan()
	if PlanID(again) != PlanID(job.Plan) {
		t.Error("the same plan has two names")
	}
	again.Units[0].Key = strings.Repeat("cd", 32)
	if PlanID(again) == PlanID(job.Plan) {
		t.Error("another key, the same plan name")
	}
	whole := testJob()
	whole.Task.Unit, whole.ShardIndex, whole.ShardTotal = "repo", 0, 1
	obj, err := TaskRunFor(whole)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range obj["spec"].(map[string]any)["params"].([]any) {
		if p.(map[string]any)["name"] == "workdir" && p.(map[string]any)["value"] != "bifrost-platform" {
			t.Errorf("the repository's own unit runs in %v", p.(map[string]any)["value"])
		}
	}
	for name, edit := range map[string]func(*Job){
		"no image":                func(j *Job) { j.Task.Inputs["image"] = "" },
		"no script":               func(j *Job) { delete(j.Task.Inputs, "script") },
		"a command with a space":  func(j *Job) { j.Task.Inputs["script"] = map[string]any{"path": "scripts/ci/unit go"} },
		"a part beyond the total": func(j *Job) { j.ShardIndex = 8 },
		"no namespace":            func(j *Job) { j.Namespace = "" },
		"a branch for a commit":   func(j *Job) { j.Plan.Commit = "main" },
		"a short key":             func(j *Job) { j.Task.Key = "abc" },
	} {
		j := testJob()
		edit(&j)
		if _, err := TaskRunFor(j); err == nil {
			t.Errorf("%s was dispatched", name)
		}
	}
}
