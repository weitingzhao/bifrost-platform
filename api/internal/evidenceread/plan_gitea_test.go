package evidenceread

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

type unitPlanTask struct {
	Unit    string          `json:"unit"`
	Task    string          `json:"task"`
	Module  string          `json:"module"`
	Key     string          `json:"input_key"`
	Shards  int             `json:"shards,omitempty"`
	Verdict string          `json:"verdict"`
	Inputs  json.RawMessage `json:"inputs,omitempty"`
}

type unitPlanAnswer struct {
	Tree   string             `json:"tree"`
	Pins   []unitplan.PlanPin `json:"pins"`
	Units  []unitPlanTask     `json:"units"`
	ToRun  int                `json:"to_run"`
	Counts map[string]int     `json:"counts"`
}

// unitPlan asks the plan route (PlanRoute) the way a request
// does and reads its answer: the plan of a commit with the ledger's word on
// each key.
func unitPlan(ctx context.Context, db eventlog.DB, trees unitexec.TreesFunc, repo, commit string, withInputs bool) (unitPlanAnswer, error) {
	route := PlanRoute{RepoOK: gitread.Name.MatchString, CommitOK: gitread.CommitID.MatchString,
		Sources: func() (PlanSources, bool) { return PlanSources{DB: db, Trees: trees}, true }}
	q := url.Values{"repo": {repo}, "commit": {commit}}
	if withInputs {
		q.Set("inputs", "1")
	}
	w := httptest.NewRecorder()
	route.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil).WithContext(ctx))
	var out unitPlanAnswer
	if w.Code != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", w.Code, w.Body.String())
	}
	return out, json.Unmarshal(w.Body.Bytes(), &out)
}

const unitsManifest = `version: 1
modules:
  - name: api
    kind: go
    root: api
    tasks: {test: {per: package}}
  - name: repo
    root: .
    tasks: {check: {}}
`

// planRepo is a small repository as Gitea serves it: every path with its
// type and git object id, and the blobs by id.
func planRepo() (entries []gitread.TreeEntry, blobs map[string]string) {
	files := map[string]string{
		"bifrost.units.yaml": unitsManifest,
		"api/go.mod":         "module example.com/m\n\ngo 1.25\n",
		"api/a/a.go":         "package a\n",
		"api/a/a_test.go":    "package a\n",
	}
	blobs = map[string]string{}
	for p, body := range files {
		id := gitBlobID(body)
		blobs[id] = body
		entries = append(entries, gitread.TreeEntry{Path: p, Mode: "100644", Type: "blob", SHA: id})
	}
	sub := func(dir string) string { // the id of a directory of blobs and the directories given
		var list []gitread.TreeEntry
		for _, e := range entries {
			if rest, ok := strings.CutPrefix(e.Path, dir+"/"); ok && !strings.Contains(rest, "/") {
				list = append(list, gitread.TreeEntry{Path: rest, Mode: e.Mode, Type: e.Type, SHA: e.SHA})
			}
		}
		id, err := gitread.TreeID(list)
		if err != nil {
			panic(err)
		}
		return id
	}
	entries = append(entries, gitread.TreeEntry{Path: "api/a", Mode: "040000", Type: "tree", SHA: sub("api/a")})
	entries = append(entries, gitread.TreeEntry{Path: "api", Mode: "040000", Type: "tree", SHA: sub("api")})
	return entries, blobs
}

// The plan of a commit read from Gitea: its tree listed once, the files it
// needs read by blob and kept, the root tree id computed; and the ledger's
// word on each key: none until a full pass is recorded for that key.
func TestUnitPlanFromGitea(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	commit := strings.Repeat("c", 40)
	entries, blobs := planRepo()
	blobReads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/bifrost/r/git/trees/"+commit:
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": commit, "page": 1, "total_count": len(entries), "truncated": false, "tree": entries})
		case strings.HasPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/")
			blobReads++
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(blobs[id]))})
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	g := unitexec.GiteaTrees(&gitread.Trees{Access: func(context.Context) (gitread.Access, error) {
		return gitread.Access{Base: srv.URL, Org: "bifrost"}, nil
	}, Client: srv.Client()})
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)

	plan, err := unitPlan(ctx, pool, g, "r", commit, true)
	if err != nil {
		t.Fatal(err)
	}
	var root []gitread.TreeEntry
	for _, e := range entries {
		if !strings.Contains(e.Path, "/") {
			root = append(root, e)
		}
	}
	wantRoot, _ := gitread.TreeID(root)
	if plan.Tree != wantRoot || len(plan.Units) != 2 || plan.ToRun != 2 || plan.Counts[evidence.EvidenceNone] != 2 {
		t.Fatalf("plan: tree %s (want %s), %+v", plan.Tree, wantRoot, plan)
	}
	var test unitPlanTask
	for _, u := range plan.Units {
		if u.Unit == "api/a" && u.Task == "test" {
			test = u
		}
	}
	if test.Key == "" || unitplan.Hash(test.Inputs) != test.Key {
		t.Fatalf("the test task's key is not the hash of its inputs: %+v", test)
	}
	reads := blobReads
	// A full pass recorded for that key: the task no longer has to run.
	inputs, _ := json.Marshal(test.Inputs)
	if _, err = pool.Exec(ctx, `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
	  VALUES ('r','api/a','test',$1,'passed','full',$2::jsonb,'{}',$3,'unit-1','uid-1')`, test.Key, string(inputs), commit); err != nil {
		t.Fatal(err)
	}
	if plan, err = unitPlan(ctx, pool, g, "r", commit, false); err != nil || plan.ToRun != 1 || plan.Counts[evidence.EvidenceGreen] != 1 {
		t.Fatalf("after a pass: %+v %v", plan, err)
	}
	if blobReads != reads {
		t.Fatalf("a second plan read %d blobs again", blobReads-reads)
	}
	if plan.Units[0].Inputs != nil {
		t.Fatal("inputs given without being asked for")
	}
	if _, err = unitPlan(ctx, pool, g, "other", commit, false); err == nil {
		t.Fatal("a repository Gitea does not have was planned")
	}
}

// gitBlobID is the git object id of a file's content, as Gitea names blobs.
func gitBlobID(content string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
