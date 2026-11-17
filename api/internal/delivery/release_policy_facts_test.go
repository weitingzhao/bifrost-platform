//bifrost:owner delivery

package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

const (
	factSHA1 = "1111111111111111111111111111111111111111"
	factSHA2 = "2222222222222222222222222222222222222222"
	factSHA3 = "3333333333333333333333333333333333333333"
)

func newFactsService(t *testing.T, cms ...*corev1.ConfigMap) PolicyFacts {
	t.Helper()
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/bifrost/repo-app/branches/main":
			_, _ = w.Write([]byte(`{"name":"main","commit":{"id":"` + factSHA1 + `"}}`))
		case "/api/v1/repos/bifrost/repo-app/tags/v1.0.0":
			_, _ = w.Write([]byte(`{"name":"v1.0.0","commit":{"sha":"` + factSHA2 + `"}}`))
		case "/api/v1/repos/bifrost/repo-app/git/commits/" + factSHA1:
			_, _ = w.Write([]byte(`{"sha":"` + factSHA1 + `"}`))
		case "/api/v1/repos/bifrost/repo-app/commits":
			commitRanges{
				factSHA1 + "|" + factSHA2: {{sha: factSHA1, files: []string{"src/b.py", "docs/a.md"}}},
				factSHA2 + "|" + factSHA1: nil,
			}.serve(w, r)
		case "/api/v1/repos/bifrost/repo-broken/branches/main":
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/repos/bifrost/repo-ops/raw/db-steps.d/a step.md":
			if r.URL.Query().Get("ref") != "main" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte("---\nid: a\n---\n"))
		case "/api/v1/repos/bifrost/repo-ops/raw/db-steps.d/big.sql":
			_, _ = w.Write([]byte(strings.Repeat("-- padding\n", 200_000)))
		case "/api/v1/repos/bifrost/repo-ops/contents/db-steps.d":
			_, _ = w.Write([]byte(`[{"name":"a step.md","type":"file"},{"name":"old","type":"dir"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(gitea.Close)
	t.Setenv("GITEA_BASE", gitea.URL)

	s := NewService(&config.ClusterEntry{ID: "test"})
	objs := make([]runtime.Object, 0, len(cms))
	for _, cm := range cms {
		objs = append(objs, cm)
	}
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(objs...))
	return s.PolicyFacts()
}

func TestPolicyFactsGitea(t *testing.T) {
	f := newFactsService(t)
	ctx := context.Background()
	if head, err := f.MainHead(ctx, "repo-app"); err != nil || head != factSHA1 {
		t.Fatalf("MainHead = %q %v", head, err)
	}
	if _, err := f.MainHead(ctx, "repo-broken"); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("broken MainHead err = %v", err)
	}
	if _, err := f.MainHead(ctx, "repo-gone"); err == nil {
		t.Fatal("missing main gave no error")
	}
	if sha, ok, err := f.TagCommit(ctx, "repo-app", "v1.0.0"); err != nil || !ok || sha != factSHA2 {
		t.Fatalf("TagCommit = %q %v %v", sha, ok, err)
	}
	if _, ok, err := f.TagCommit(ctx, "repo-app", "v9"); err != nil || ok {
		t.Fatalf("missing tag = %v %v", ok, err)
	}
	if ok, err := f.CommitExists(ctx, "repo-app", factSHA1); err != nil || !ok {
		t.Fatalf("CommitExists = %v %v", ok, err)
	}
	if ok, err := f.CommitExists(ctx, "repo-app", factSHA3); err != nil || ok {
		t.Fatalf("unknown commit = %v %v", ok, err)
	}
	files, err := f.ChangedFiles(ctx, "repo-app", factSHA2, factSHA1)
	if err != nil || strings.Join(files, ",") != "docs/a.md,src/b.py" {
		t.Fatalf("ChangedFiles = %v %v", files, err)
	}
	if _, err := f.ChangedFiles(ctx, "repo-app", factSHA3, factSHA1); err == nil {
		t.Fatal("an unreadable range gave no error")
	}
}

func TestPolicyFactsWindowAndConfigMaps(t *testing.T) {
	window := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"},
		Data:       map[string]string{"window.json": `{"who":"agent@mac","what":"bifrost-research"}`},
	}
	freeze := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "bifrost-release-freeze", Namespace: "cicd"},
		Data:       map[string]string{"frozen": "false"},
	}
	f := newFactsService(t, window, freeze)
	ctx := context.Background()
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", "agent@mac"); msg != "" {
		t.Fatalf("holder refused: %s", msg)
	}
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", "someone@else"); !strings.Contains(msg, "held by someone else") {
		t.Fatalf("other caller = %q", msg)
	}
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", ""); !strings.Contains(msg, "no who") {
		t.Fatalf("empty who = %q", msg)
	}
	if data, found, err := f.ConfigMap(ctx, "bifrost-release-freeze"); err != nil || !found || data["frozen"] != "false" {
		t.Fatalf("freeze = %v %v %v", data, found, err)
	}
	if _, found, err := f.ConfigMap(ctx, "bifrost-release-policy"); err != nil || found {
		t.Fatalf("missing policy = %v %v", found, err)
	}

	none := newFactsService(t)
	if msg := none.HeldBy(ctx, "bifrost-deliver-research", "agent@mac"); !strings.Contains(msg, "no release window is open") {
		t.Fatalf("no window = %q", msg)
	}
}

// The policy's window clause holds only for a live window that covers the
// pipeline and is held by the caller. The pipelines here need not hold a
// window to start a run (windowPipelines: required false), which is the case
// where the start gate's rule would let any who through.
func TestPolicyFactsHeldByNeedsALiveWindowHeldByTheCaller(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	past, future := now.Add(-time.Minute).Format(time.RFC3339), now.Add(5*time.Minute).Format(time.RFC3339)
	record := func(body string) PolicyFacts {
		return newFactsService(t, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"},
			Data:       map[string]string{"window.json": body},
		})
	}
	live := `{"who":"agent@mac","what":"bifrost-platform","started_at":"` + past + `","expires_at":"` + future + `"}`
	expired := `{"who":"agent@mac","what":"bifrost-platform,bifrost-ui","started_at":"` + past + `","expires_at":"` + past + `"}`
	released := `{"who":"agent@mac","what":"bifrost-platform","started_at":"` + past + `","expires_at":"` + past + `","ended_at":"` + past + `"}`
	const noWindow = "no release window is open"
	for _, tc := range []struct {
		name, body, pipeline, who, want string
	}{
		{"expired record, any who", expired, "bifrost-deliver-platform-prod", "anyone", noWindow},
		{"expired record, its old holder", expired, "bifrost-deliver-platform-prod", "agent@mac", noWindow},
		{"expired record, no who", expired, "bifrost-deliver-platform-prod", "", noWindow},
		{"released record", released, "bifrost-deliver-platform", "agent@mac", noWindow},
		{"expired record, staging", expired, "bifrost-deliver-stg", "anyone", noWindow},
		{"expired record, a pipeline that must hold one", expired, "bifrost-deliver-research", "agent@mac", noWindow},
		{"record without a holder", `{"what":"bifrost-platform","expires_at":"` + future + `"}`, "bifrost-deliver-platform-prod", "anyone", noWindow},
		{"empty record", "", "bifrost-deliver-platform-prod", "anyone", noWindow},
		{"live, the holder", live, "bifrost-deliver-platform-prod", "agent@mac", ""},
		{"live, the holder, spaces", live, "bifrost-deliver-platform", " agent@mac ", ""},
		{"live, someone else", live, "bifrost-deliver-platform-prod", "anyone", "held by someone else (who=agent@mac what=bifrost-platform"},
		{"live, no who", live, "bifrost-deliver-platform-prod", "", "no who"},
		{"live, another release", live, "bifrost-deliver-stg", "agent@mac", "does not cover bifrost-deliver-stg: it needs one of bifrost-trade-api"},
		{"live, a pipeline outside the table", live, "bifrost-smoke", "agent@mac", "does not cover bifrost-smoke: no release window covers it"},
	} {
		got := record(tc.body).HeldBy(ctx, tc.pipeline, tc.who)
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: HeldBy(%s, %q) = %q, want %q", tc.name, tc.pipeline, tc.who, got, tc.want)
		}
	}
	if got := newFactsService(t).HeldBy(ctx, "bifrost-deliver-platform-prod", "anyone"); !strings.Contains(got, noWindow) {
		t.Errorf("no ConfigMap: %q", got)
	}
}

func TestPolicyFactsReadFilesAndWriteConfigMaps(t *testing.T) {
	f := newFactsService(t)
	ctx := context.Background()
	text, found, err := f.FileAt(ctx, "repo-ops", "main", "db-steps.d/a step.md")
	if err != nil || !found || !strings.Contains(text, "id: a") {
		t.Fatalf("FileAt = %q %v %v", text, found, err)
	}
	if _, found, err = f.FileAt(ctx, "repo-ops", factSHA1, "db-steps.d/a step.md"); err != nil || found {
		t.Fatalf("FileAt at a ref without the file = %v %v", found, err)
	}
	// Cleanup wave 1 (2026-11-11): a file past the old 1 MiB bound is read whole.
	var big string
	if big, found, err = f.FileAt(ctx, "repo-ops", "main", "db-steps.d/big.sql"); err != nil || !found || len(big) != 2_200_000 {
		t.Fatalf("FileAt of a 2.2 MB file = %d bytes %v %v", len(big), found, err)
	}
	names, err := f.ListDir(ctx, "repo-ops", "main", "db-steps.d")
	if err != nil || strings.Join(names, ",") != "a step.md" {
		t.Fatalf("ListDir = %q %v", names, err)
	}
	if _, err = f.ListDir(ctx, "repo-ops", "main", "nowhere"); err == nil {
		t.Fatal("ListDir of a missing dir did not fail")
	}

	if err = f.WriteConfigMap(ctx, "bifrost-release-freeze", map[string]string{"frozen": "true"}); err != nil {
		t.Fatal(err)
	}
	if err = f.WriteConfigMap(ctx, "bifrost-release-freeze", map[string]string{"frozen": "false"}); err != nil {
		t.Fatal(err)
	}
	data, found, err := f.ConfigMap(ctx, "bifrost-release-freeze")
	if err != nil || !found || data["frozen"] != "false" || len(data) != 1 {
		t.Fatalf("ConfigMap after two writes = %v %v %v", data, found, err)
	}
}
