package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

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
func planRepo() (entries []commitTreeEntry, blobs map[string]string) {
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
		entries = append(entries, commitTreeEntry{Path: p, Mode: "100644", Type: "blob", SHA: id})
	}
	sub := func(dir string) string { // the id of a directory of blobs and the directories given
		var list []commitTreeEntry
		for _, e := range entries {
			if rest, ok := strings.CutPrefix(e.Path, dir+"/"); ok && !strings.Contains(rest, "/") {
				list = append(list, commitTreeEntry{Path: rest, Mode: e.Mode, Type: e.Type, SHA: e.SHA})
			}
		}
		id, err := gitTreeID(list)
		if err != nil {
			panic(err)
		}
		return id
	}
	entries = append(entries, commitTreeEntry{Path: "api/a", Mode: "040000", Type: "tree", SHA: sub("api/a")})
	entries = append(entries, commitTreeEntry{Path: "api", Mode: "040000", Type: "tree", SHA: sub("api")})
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
	g := &giteaTrees{access: func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL, Org: "bifrost"}, nil
	}, client: srv.Client()}
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)

	plan, err := unitPlan(ctx, pool, g.tree, "r", commit, true)
	if err != nil {
		t.Fatal(err)
	}
	var root []commitTreeEntry
	for _, e := range entries {
		if !strings.Contains(e.Path, "/") {
			root = append(root, e)
		}
	}
	wantRoot, _ := gitTreeID(root)
	if plan.Tree != wantRoot || len(plan.Units) != 2 || plan.ToRun != 2 || plan.Counts[store.EvidenceNone] != 2 {
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
	if plan, err = unitPlan(ctx, pool, g.tree, "r", commit, false); err != nil || plan.ToRun != 1 || plan.Counts[store.EvidenceGreen] != 1 {
		t.Fatalf("after a pass: %+v %v", plan, err)
	}
	if blobReads != reads {
		t.Fatalf("a second plan read %d blobs again", blobReads-reads)
	}
	if plan.Units[0].Inputs != nil {
		t.Fatal("inputs given without being asked for")
	}
	if _, err = unitPlan(ctx, pool, g.tree, "other", commit, false); err == nil {
		t.Fatal("a repository Gitea does not have was planned")
	}
}
