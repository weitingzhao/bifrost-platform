package agentrtapi_test

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /artifacts (W-31 2.3 search_artifacts): any reader lists metadata,
// newest first, never a body; an agentd token is shown no exam artifact; a
// query that cannot be answered is 400; no token is 401.
func TestSearchArtifactsRoute(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	var exam, handover string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		if exam, err = store.PutArtifact(ctx, tx, "controller", "exam", "text/plain", []byte("an answer")); err != nil {
			return err
		}
		handover, err = store.PutArtifact(ctx, tx, "controller", "handover", "text/markdown", []byte("# handover"))
		return err
	}))
	ids := func(out map[string]any) []string {
		var got []string
		for _, e := range out["artifacts"].([]any) {
			m := e.(map[string]any)
			if _, body := m["body"]; body {
				t.Fatalf("a body in the listing: %v", m)
			}
			got = append(got, m["artifact_id"].(string))
		}
		return got
	}
	has := func(list []string, id string) bool {
		for _, x := range list {
			if x == id {
				return true
			}
		}
		return false
	}
	code, out := call(t, s, "viewer", "GET", "artifacts?limit=100", nil)
	if code != http.StatusOK || out["next"] != "" || !has(ids(out), exam) || !has(ids(out), handover) {
		t.Fatalf("a viewer: %d %v", code, out)
	}
	first := out["artifacts"].([]any)[0].(map[string]any)
	for _, f := range []string{"artifact_id", "artifact_kind", "media_type", "size_bytes", "created_by_actor_id", "created_at"} {
		if _, ok := first[f]; !ok {
			t.Fatalf("no %s in %v", f, first)
		}
	}
	if code, out = call(t, s, "agentd:h1", "GET", "artifacts?limit=100", nil); code != http.StatusOK || has(ids(out), exam) || !has(ids(out), handover) {
		t.Fatalf("an agentd token: %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "artifacts?kind=handover", nil); code != http.StatusOK || len(ids(out)) != 1 || ids(out)[0] != handover {
		t.Fatalf("kind handover: %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "artifacts?task="+task, nil); code != http.StatusOK || len(ids(out)) != 1 {
		t.Fatalf("the task's: %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "artifacts?limit=1", nil); code != http.StatusOK || len(ids(out)) != 1 || out["next"] == "" {
		t.Fatalf("a page of one: %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "artifacts?limit=1&before="+out["next"].(string), nil); code != http.StatusOK || len(ids(out)) != 1 {
		t.Fatalf("the next page: %d %v", code, out)
	}
	for _, q := range []string{"kind=patch", "task=t1", "mission=M1", "author=nobody", "since=yesterday", "limit=0", "limit=101", "before=x"} {
		if code, out = call(t, s, "viewer", "GET", "artifacts?"+q, nil); code != http.StatusBadRequest || out["error"] != "bad_request" {
			t.Errorf("%s: %d %v", q, code, out)
		}
	}
	if code, _ = call(t, s, "", "GET", "artifacts", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
}
