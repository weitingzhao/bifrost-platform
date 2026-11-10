package agentrtapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestWaveRoutes(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool, func(a *agentrtapi.API) {
		a.Head = func(_ context.Context, repo, ref string) (string, error) {
			if repo != "bifrost-platform" || ref != "refs/heads/main" {
				t.Errorf("head: %s %s", repo, ref)
			}
			return commitA, nil
		}
	})
	for _, repo := range []string{"other", ""} {
		if code, out := call(t, s, "admin", "POST", "waves", map[string]any{"repo": repo, "plan_root": "root"}); code != 400 {
			t.Fatalf("repo: %d %v", code, out)
		}
	}
	for _, action := range []string{"close", "abandon"} {
		code, out := call(t, s, "admin", "POST", "waves", map[string]any{"repo": "bifrost-platform", "plan_root": "root"})
		if code != 200 || out["base"] != commitA || out["state"] != "open" {
			t.Fatalf("open: %d %v", code, out)
		}
		id := out["id"].(string)
		if code, out = call(t, s, "admin", "POST", "waves/"+id+"/"+action, nil); code != 200 {
			t.Fatalf("%s: %d %v", action, code, out)
		}
		if code, out = call(t, s, "admin", "POST", "waves/"+id+"/close", nil); code != 422 {
			t.Fatalf("closed: %d %v", code, out)
		}
	}
	// The list is the ledger fold.
	req, err := http.NewRequest("GET", s.URL+"/api/v1/agentrt/waves?repo=bifrost-platform", nil)
	must(t, err)
	req.Header.Set("Authorization", "Bearer "+tokens["admin"])
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var fold []store.Wave
	must(t, json.NewDecoder(resp.Body).Decode(&fold))
	if resp.StatusCode != 200 || len(fold) != 2 || fold[0].State != "closed" || fold[1].State != "abandoned" {
		t.Fatalf("list: %d %+v", resp.StatusCode, fold)
	}
	for _, state := range []string{"closed", "open", "abandoned"} {
		t.Run("list state "+state, func(t *testing.T) {
			req, err := http.NewRequest("GET", s.URL+"/api/v1/agentrt/waves?repo=bifrost-platform&state="+state, nil)
			must(t, err)
			req.Header.Set("Authorization", "Bearer "+tokens["admin"])
			resp, err := s.Client().Do(req)
			must(t, err)
			defer resp.Body.Close()
			var filtered []store.Wave
			must(t, json.NewDecoder(resp.Body).Decode(&filtered))
			want := []store.Wave{}
			for _, wave := range fold {
				if wave.State == state {
					want = append(want, wave)
				}
			}
			if resp.StatusCode != 200 || filtered == nil || len(filtered) != len(want) {
				t.Fatalf("list state %s: %d %+v", state, resp.StatusCode, filtered)
			}
			for i := range want {
				if filtered[i] != want[i] {
					t.Fatalf("list state %s: got %+v, want %+v", state, filtered, want)
				}
			}
		})
	}
	if code, out := call(t, s, "admin", "GET", "waves?state=x", nil); code != 400 || out["error"] != "bad_request" || out["message"] != "state is open, closed or abandoned" {
		t.Fatalf("list state invalid: %d %v", code, out)
	}
	if code, out := call(t, s, "admin", "GET", "waves?repo=other", nil); code != 400 {
		t.Fatalf("list repo: %d %v", code, out)
	}
	wave, err := store.OpenWave(ctx, pool, store.Writer{Actor: "owner"}, "bifrost-platform", "root", func(context.Context, string, string) (string, error) { return commitA, nil })
	must(t, err)
	var specID string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		specID, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("wave task"))
		return err
	}))
	for i, edit := range []func(map[string]any){
		func(b map[string]any) { b["urgent"] = true },
		func(b map[string]any) { b["repo"] = "other" },
		func(b map[string]any) { b["wave"] = "missing" },
		func(b map[string]any) { b["wave"] = fold[0].ID },
		func(b map[string]any) { b["effect_class"] = "none" },
	} {
		task := map[string]any{"ref": "x", "plan_node_id": "R2:1-1", "role": "other", "task_class": "go", "title": "task", "spec_artifact_id": specID, "effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA, "wave": wave.ID}
		edit(task)
		code, out := call(t, s, "admin", "POST", "missions/M-1/tasks", map[string]any{"idempotency_key": fmt.Sprintf("wave-refusal-%d", i), "tasks": []any{task}})
		if code != 422 || !strings.Contains(fmt.Sprint(out["message"]), "wave") {
			t.Fatalf("task refusal %d: %d %v", i, code, out)
		}
	}
	taskID := store.NewTaskID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("task"))
		if err != nil {
			return err
		}
		_, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: taskID, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement", Class: "go", Title: "t", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA, Wave: wave.ID})
		if err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		// Not yet accepted: the close is refused (hold and release, 2026-11-10).
		_, err = tx.Exec(ctx, `UPDATE agentrt.task SET state='running' WHERE task_id=$1`, taskID)
		return err
	}))
	if code, out := call(t, s, "admin", "POST", "waves/"+wave.ID+"/close", nil); code != 422 || !strings.Contains(fmt.Sprint(out["message"]), taskID) {
		t.Fatalf("pending: %d %v", code, out)
	}
	if code, out := call(t, s, "admin", "POST", "waves/"+wave.ID+"/abandon", nil); code != 200 {
		t.Fatalf("abandon pending: %d %v", code, out)
	}
}
