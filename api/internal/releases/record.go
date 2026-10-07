package releases

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RepoBuild is the commit one repo was built from.
type RepoBuild struct {
	SHA string `json:"sha"`
	// Source: "result" (the clone task's commit result), "pinned" (the run's
	// inline spec pinned that clone to a full SHA) or "param" (the clone's own
	// revision parameter is a full SHA; for a TaskRun without one, the run's
	// revision parameter, which every clone of such a pipelineRef run used).
	Source string `json:"source"`
}

// Record is one finished delivery run.
type Record struct {
	Run         string               `json:"run"`
	Pipeline    string               `json:"pipeline"`
	Lane        string               `json:"lane"`
	Env         string               `json:"env"`
	Deploys     bool                 `json:"deploys"`
	Revision    string               `json:"revision,omitempty"`
	Tag         string               `json:"tag,omitempty"`
	FromRun     string               `json:"from_run,omitempty"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt time.Time            `json:"completed_at"`
	Repos       map[string]RepoBuild `json:"repos"`
	// Missing lists clone tasks whose commit could not be read (runs older
	// than the commit result, cloned at a moving ref).
	Missing    []string  `json:"missing,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

const (
	labelRecord = "bifrost.io/release-record"
	labelLane   = "bifrost.io/release-lane"
	labelEnv    = "bifrost.io/release-env"
	labelRun    = "bifrost.io/release-run"
	// labelComplete is "false" while some clone's commit is unknown; such a record
	// is rebuilt while its run still exists.
	labelComplete = "bifrost.io/release-complete"
	dataKey       = "record.json"
	namePrefix    = "release-"
	// recordVersion bumps when Record changes shape.
	recordVersion = "v1"
)

var (
	fullSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoInURL = regexp.MustCompile(`/([^/]+?)(?:\.git)?/?$`)
	labelSafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

// succeeded: Tekton reports Succeeded, or Completed when a run skipped its finally tasks.
func succeeded(run *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Succeeded" {
			return m["status"] == "True"
		}
	}
	return false
}

func nestedTime(obj map[string]any, fields ...string) time.Time {
	s, _, _ := unstructured.NestedString(obj, fields...)
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func runParam(run *unstructured.Unstructured, name string) string {
	params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, p := range params {
		m, _ := p.(map[string]any)
		if m["name"] == name {
			if v, ok := m["value"].(string); ok {
				return v
			}
		}
	}
	return ""
}

// pinnedClones reads full-SHA revisions from an inline pipelineSpec's clone tasks:
// pipelineTask name -> sha.
func pinnedClones(run *unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	tasks, _, _ := unstructured.NestedSlice(run.Object, "spec", "pipelineSpec", "tasks")
	for _, t := range tasks {
		tm, _ := t.(map[string]any)
		name, _ := tm["name"].(string)
		params, _ := tm["params"].([]any)
		for _, p := range params {
			pm, _ := p.(map[string]any)
			if v, ok := pm["value"].(string); ok && pm["name"] == "revision" && fullSHA.MatchString(v) {
				out[name] = v
			}
		}
	}
	return out
}

// cloneOf reads one TaskRun: the repo it cloned, the commit it produced and the
// resolved revision param it was given (hasRev is false when it had none).
// ok is false for TaskRuns that are not a clone (no url param).
func cloneOf(tr *unstructured.Unstructured) (pipelineTask, repo, sha, rev string, hasRev, ok bool) {
	pipelineTask = tr.GetLabels()["tekton.dev/pipelineTask"]
	params, _, _ := unstructured.NestedSlice(tr.Object, "spec", "params")
	var url string
	for _, p := range params {
		m, _ := p.(map[string]any)
		switch m["name"] {
		case "url":
			url, _ = m["value"].(string)
		case "revision":
			rev, _ = m["value"].(string)
			rev, hasRev = strings.TrimSpace(rev), true
		}
	}
	if url == "" {
		return pipelineTask, "", "", "", false, false
	}
	if m := repoInURL.FindStringSubmatch(url); m != nil {
		repo = m[1]
	}
	results, _, _ := unstructured.NestedSlice(tr.Object, "status", "results")
	for _, r := range results {
		m, _ := r.(map[string]any)
		if m["name"] == "commit" {
			if v, ok := m["value"].(string); ok && fullSHA.MatchString(strings.TrimSpace(v)) {
				sha = strings.TrimSpace(v)
			}
		}
	}
	return pipelineTask, repo, sha, rev, hasRev, repo != ""
}

// buildRecord assembles a Record from a finished run and its TaskRuns.
func buildRecord(run *unstructured.Unstructured, rule Rule, taskRuns []unstructured.Unstructured, now time.Time) Record {
	rec := Record{
		Run:         run.GetName(),
		Pipeline:    run.GetLabels()["tekton.dev/pipeline"],
		Lane:        rule.Lane,
		Env:         rule.Env,
		Deploys:     rule.Deploys,
		Revision:    runParam(run, "revision"),
		Tag:         runParam(run, "tag"),
		FromRun:     run.GetLabels()["bifrost.io/from-stg-run"],
		StartedAt:   nestedTime(run.Object, "status", "startTime"),
		CompletedAt: nestedTime(run.Object, "status", "completionTime"),
		Repos:       map[string]RepoBuild{},
		RecordedAt:  now,
	}
	pinned := pinnedClones(run)
	paramSHA := ""
	// The run's revision stands in only for a clone TaskRun that carries no revision of its
	// own: a pipeline may clone some repos at another param (bifrost-ui at uiRevision).
	if _, inline, _ := unstructured.NestedMap(run.Object, "spec", "pipelineSpec"); !inline && fullSHA.MatchString(rec.Revision) {
		paramSHA = rec.Revision
	}
	for i := range taskRuns {
		task, repo, sha, rev, hasRev, ok := cloneOf(&taskRuns[i])
		if !ok {
			continue
		}
		switch {
		case sha != "":
			rec.Repos[repo] = RepoBuild{SHA: sha, Source: "result"}
		case pinned[task] != "":
			rec.Repos[repo] = RepoBuild{SHA: pinned[task], Source: "pinned"}
		case fullSHA.MatchString(rev):
			rec.Repos[repo] = RepoBuild{SHA: rev, Source: "param"}
		case !hasRev && paramSHA != "":
			rec.Repos[repo] = RepoBuild{SHA: paramSHA, Source: "param"}
		default:
			rec.Missing = append(rec.Missing, task)
		}
	}
	return rec
}

func recordName(run string) string { return namePrefix + run }

func labelValue(s string) string {
	s = labelSafe.ReplaceAllString(s, "-")
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.Trim(s, "-._")
}

func encode(rec Record) (string, error) {
	b, err := json.Marshal(rec)
	return string(b), err
}
