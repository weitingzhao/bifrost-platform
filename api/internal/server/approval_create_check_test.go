package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// TD-420 (approval #80): a start_pipeline_run request is checked when it is
// made. Every full SHA it signs goes through the run's per-repository ref
// check; a missing one is refused with the repository and the SHA, and no
// approval row is written. The right SHA makes the request.
func TestPipelineRunRequestChecksItsCommitsWhenMade(t *testing.T) {
	const good, goodUI = "b6b565f2368e3f5ec39d09f288bfc0fd50a115b1", "1f0e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
	wrong, wrongUI := good[:39]+"2", goodUI[:39]+"d" // one character off, as #80's was
	var asked []string
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/bifrost-platform/git/commits/"+good),
			strings.HasSuffix(r.URL.Path, "/bifrost-ui/git/commits/"+goodUI):
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": good})
		case strings.Contains(r.URL.Path, "/git/commits/"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/branches"), strings.HasSuffix(r.URL.Path, "/tags"):
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gitea.Close)
	t.Setenv("GITEA_BASE", gitea.URL)
	dyn := pipelineDyn(t)
	del := delivery.NewService(&config.ClusterEntry{})
	del.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	del.SetClientsetForTest(fake.NewSimpleClientset())
	s := &Server{delivery: delivery.NewHandlerForTest(del, nil)}
	svc := approvals.New(filepath.Join(t.TempDir(), "approvals"), nil)
	svc.SetCreateCheck(s.approvalCreateCheck)
	h := serveApprovals(svc)
	request := func(revision, ui string) *httptest.ResponseRecorder {
		params := map[string]any{"name": prodPipeline, "revision": revision, "who": "owner"}
		if ui != "" {
			params["params"] = map[string]any{"uiRevision": ui}
		}
		body, err := json.Marshal(map[string]any{"action": "start_pipeline_run", "reason": "ship", "params": params})
		if err != nil {
			t.Fatal(err)
		}
		return doApproval(h, http.MethodPost, "/approvals", string(body))
	}

	for name, c := range map[string]struct{ revision, ui, repo, sha string }{
		"the revision": {wrong, goodUI, "bifrost-platform", wrong},
		"a uiRevision": {good, wrongUI, "bifrost-ui", wrongUI},
		// the platform's commit in the UI's repository is missing there
		"the platform's commit as the UI's": {good, good, "bifrost-ui", good},
		// the run's input rules hold at creation too, its param rules with them,
		// whether or not a full SHA is signed (review 510 B1)
		"a short SHA":         {good[:12], goodUI, "full 40-character", good[:12]},
		"a uiRevision branch": {"main", "main", "uiRevision must be a 40-character", "uiRevision"},
	} {
		rec := request(c.revision, c.ui)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "refused at creation") ||
			!strings.Contains(rec.Body.String(), c.repo) || !strings.Contains(rec.Body.String(), c.sha) {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if n := len(svc.Pending()); n != 0 {
			t.Fatalf("%s: %d approval rows after a refused request", name, n)
		}
	}
	asked = nil
	if rec := request(good, goodUI); rec.Code != http.StatusCreated {
		t.Fatalf("the right SHA: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(svc.Pending()); n != 1 {
		t.Fatalf("%d approval rows after the right SHA", n)
	}
	if strings.Join(asked, " ") != "/api/v1/repos/bifrost/bifrost-platform/git/commits/"+good+" /api/v1/repos/bifrost/bifrost-ui/git/commits/"+goodUI {
		t.Fatalf("the right SHAs were not each looked up in their repository: %v", asked)
	}
	// A branch is not a signed commit: it may move before the run, which checks it.
	asked = nil
	if rec := request("main", ""); rec.Code != http.StatusCreated || len(asked) != 0 {
		t.Fatalf("a branch: %d %s, Gitea asked %v", rec.Code, rec.Body.String(), asked)
	}
}
