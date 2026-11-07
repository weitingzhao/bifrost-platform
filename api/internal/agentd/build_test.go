package agentd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

const buildCommit = "0123456789abcdef0123456789abcdef01234567"

// The build agentd reports is the commit Go stamped in it, whether its work
// tree had changes, and the sha256 of its binary; a binary that names no
// commit has no build to report.
func TestTheBuildIsTheCommitAndTheBinarysSHA256(t *testing.T) {
	self := filepath.Join(t.TempDir(), "agentd")
	if err := os.WriteFile(self, []byte("a binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("a binary"))
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: buildCommit}, {Key: "vcs.modified", Value: "true"}}}
	b, err := agentd.BuildOf(info, self)
	if err != nil || *b != (agentd.Build{Commit: buildCommit, SHA256: hex.EncodeToString(sum[:]), Modified: true}) {
		t.Fatalf("build %+v %v", b, err)
	}
	if b, err := agentd.BuildOf(&debug.BuildInfo{}, self); err == nil {
		t.Fatalf("a build without its commit: %+v", b)
	}
	if b, err := agentd.BuildOf(info, filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatalf("a build without its binary: %+v", b)
	}
}

// A claim carries the client's build; a platform from before it refuses the
// field (400, unknown field), and the claim goes again at once without it, as
// do the client's later claims, so the host keeps taking work while the
// platform is older than agentd. A platform older than the free disk too
// refuses one field, then the other, and the claim still goes.
func TestAClaimCarriesTheBuildAnOldPlatformIsSpared(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	refused := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		field := ""
		for _, f := range []string{"build", "disk_free_bytes"} { // the order the client's body has them
			if refused[f] && b[f] != nil {
				field = f
				break
			}
		}
		mu.Unlock()
		if field != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"` + field + `\""}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	build := &agentd.Build{Commit: buildCommit, SHA256: strings.Repeat("ab", 32)}
	disk := &agentd.Disk{Host: 3 << 30, Root: 2 << 30}
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture", Build: build}
	if cl, err := c.ClaimWith(ctx, 1, []agentd.Tool{cursor}, disk); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	want := map[string]any{"commit": buildCommit, "sha256": strings.Repeat("ab", 32), "modified": false}
	if b, ok := bodies[0]["build"].(map[string]any); !ok || len(b) != 3 || b["commit"] != want["commit"] || b["sha256"] != want["sha256"] ||
		b["modified"] != false || bodies[0]["disk_free_bytes"] == nil {
		t.Fatalf("the claim's body %v", bodies[0])
	}

	refused["build"] = true
	o := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture", Build: build}
	for i := 0; i < 2; i++ {
		if cl, err := o.ClaimWith(ctx, 1, []agentd.Tool{cursor}, disk); err != nil || cl != nil {
			t.Fatalf("claim %d to an old platform: %+v %v", i, cl, err)
		}
	}
	mu.Lock()
	if len(bodies) != 4 || bodies[1]["build"] == nil || bodies[2]["build"] != nil || bodies[2]["disk_free_bytes"] == nil ||
		bodies[3]["build"] != nil || bodies[3]["disk_free_bytes"] == nil {
		t.Fatalf("to an old platform: %v", bodies[1:])
	}
	refused["disk_free_bytes"] = true
	mu.Unlock()

	older := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture", Build: build}
	for i := 0; i < 2; i++ {
		if cl, err := older.ClaimWith(ctx, 1, []agentd.Tool{cursor}, disk); err != nil || cl != nil {
			t.Fatalf("claim %d to an older platform: %+v %v", i, cl, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 8 || bodies[4]["build"] == nil || bodies[5]["build"] != nil || bodies[5]["disk_free_bytes"] == nil ||
		bodies[6]["build"] != nil || bodies[6]["disk_free_bytes"] != nil || bodies[7]["build"] != nil || bodies[7]["disk_free_bytes"] != nil {
		t.Fatalf("to a platform older than both: %v", bodies[4:])
	}
}
