package unitexec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The dispatching half: one planned task, or one part of it, as the TaskRun
// the platform creates. Nothing here talks to the cluster; the caller creates
// what TaskRunFor returns.
//
// A TaskRun the platform creates may carry only plain-token parameters (the
// admission policy bifrost-platform-taskrun in infra's platform-rbac: letters,
// digits and ._/:@+=- , at most 256, no leading dash). So what runs is a
// script of the tenant's repository, named by its path, and a job whose
// values would be refused is refused here, before the cluster is asked.

// More labels a unit TaskRun carries (see receive.go for the rest).
const (
	LabelTier      = "bifrost.io/ci-tier"
	LabelPurpose   = "bifrost.io/purpose"
	purposeCIUnit  = "ci-unit"
	sourceRoot     = "/unit/src"
	defaultTimeout = "30m0s"
)

// Job is one TaskRun to create.
type Job struct {
	Plan *unitplan.Plan
	Task unitplan.PlannedTask
	// Part ShardIndex of ShardTotal of the task's tests; 0 of 1 is all of it.
	ShardIndex, ShardTotal int
	// Attempt is 1, or 2 for the one retry of a run the cluster lost.
	Attempt int
	// Seq numbers the plan's jobs; with the plan it names the TaskRun.
	Seq int
	// Tier is the ci-tier label: branch, merge or nightly.
	Tier string
	// GiteaBase is the clone URL's prefix: http://gitea.../<org>
	GiteaBase string
	// NodeSelector places the pod; empty leaves it to the scheduler.
	NodeSelector map[string]string
	Namespace    string
}

var plainToken = regexp.MustCompile(`^([\p{L}\p{N}._/:@+=][\p{L}\p{N}._/:@+=-]{0,255})?$`)

// PlanID names a plan: the same repository, commit and keys give the same
// name, so a plan computed twice is one plan.
func PlanID(p *unitplan.Plan) string {
	keys := make([]string, 0, len(p.Units))
	for _, u := range p.Units {
		keys = append(keys, u.Unit+"\x00"+u.Task+"\x00"+u.Key)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(p.Repo + "\n" + p.Commit + "\n" + strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// RunName is the TaskRun's name: the plan, the job's number, and r2 on the
// retry. Never a clock: two jobs of one second must not meet.
func RunName(planID string, seq, attempt int) string {
	name := fmt.Sprintf("unit-%s-%d", planID[:8], seq)
	if attempt > 1 {
		name += "-r" + strconv.Itoa(attempt)
	}
	return name
}

// TaskRunFor builds the TaskRun of a job.
func TaskRunFor(j Job) (map[string]any, error) {
	if j.Plan == nil {
		return nil, fmt.Errorf("unitexec: a job needs its plan")
	}
	in := j.Task.Inputs
	text := func(name string) string { s, _ := in[name].(string); return s }
	template, image := text("template"), text("image")
	script, _ := in["script"].(map[string]any)
	path, _ := script["path"].(string)
	switch {
	case template == "" || image == "" || path == "":
		return nil, fmt.Errorf("unitexec: %s %s cannot be dispatched: the plan gives it template %q, image %q, script %q", j.Task.Unit, j.Task.Task, template, image, path)
	case j.ShardTotal < 1 || j.ShardIndex < 0 || j.ShardIndex >= j.ShardTotal:
		return nil, fmt.Errorf("unitexec: part %d of %d", j.ShardIndex, j.ShardTotal)
	case j.Attempt < 1 || j.Seq < 0 || j.Namespace == "" || j.GiteaBase == "":
		return nil, fmt.Errorf("unitexec: a job needs an attempt, a number, a namespace and the Gitea base")
	case !sha256Hex.MatchString(j.Task.Key) || !sha1Hex.MatchString(j.Plan.Commit):
		return nil, fmt.Errorf("unitexec: %s %s: key %q or commit %q is not a hash", j.Task.Unit, j.Task.Task, j.Task.Key, j.Plan.Commit)
	}
	planID := PlanID(j.Plan)
	workdir := j.Plan.Repo
	if j.Task.Unit != "repo" {
		workdir += "/" + j.Task.Unit
	}
	pins := make([]string, 0, len(j.Plan.Pins))
	for _, p := range j.Plan.Pins {
		pins = append(pins, p.Repo+"@"+p.Commit)
	}
	sort.Strings(pins)
	params := [][2]string{
		{"repo-url", strings.TrimRight(j.GiteaBase, "/") + "/" + j.Plan.Repo + ".git"},
		{"revision", j.Plan.Commit},
		{"extra-clones", strings.Join(pins, "+")},
		{"image", image},
		{"workdir", workdir},
		{"command", sourceRoot + "/" + j.Plan.Repo + "/" + path},
		{"shard-index", strconv.Itoa(j.ShardIndex)},
		{"shard-total", strconv.Itoa(j.ShardTotal)},
		{"unit", j.Task.Unit},
		{"task", j.Task.Task},
	}
	if roles := text("db_roles"); roles != "" && strings.HasSuffix(template, "-db") {
		params = append(params, [2]string{"db-roles", roles})
	}
	list := make([]any, 0, len(params))
	for _, p := range params {
		if !plainToken.MatchString(p[1]) {
			return nil, fmt.Errorf("unitexec: %s %s: parameter %s=%q is not a plain token the cluster admits from the platform", j.Task.Unit, j.Task.Task, p[0], p[1])
		}
		list = append(list, map[string]any{"name": p[0], "value": p[1]})
	}
	labels := map[string]any{
		LabelPlan: planID, LabelRepo: j.Plan.Repo, LabelTask: j.Task.Task, LabelKey: j.Task.Key[:32],
		LabelAttempt: strconv.Itoa(j.Attempt), LabelPurpose: purposeCIUnit,
	}
	if j.Tier != "" {
		labels[LabelTier] = j.Tier
	}
	spec := map[string]any{
		"taskRef": map[string]any{"name": template},
		"params":  list,
		"timeout": defaultTimeout,
	}
	if len(j.NodeSelector) > 0 {
		selector := make(map[string]any, len(j.NodeSelector))
		for k, v := range j.NodeSelector {
			selector[k] = v
		}
		spec["podTemplate"] = map[string]any{"nodeSelector": selector}
	}
	return map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "TaskRun",
		"metadata": map[string]any{
			"name":        RunName(planID, j.Seq, j.Attempt),
			"namespace":   j.Namespace,
			"labels":      labels,
			"annotations": map[string]any{AnnotationUnit: j.Task.Unit, AnnotationKey: j.Task.Key},
		},
		"spec": spec,
	}, nil
}
