package agentd_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// TD-415: the free disk agentd reads is what the volumes have free.
func TestMeasureDiskReadsFreeBytes(t *testing.T) {
	d, err := agentd.MeasureDisk(t.TempDir())
	if err != nil || d.Host <= 0 || d.Root <= 0 {
		t.Fatalf("disk %+v %v", d, err)
	}
}

// TD-415: a claim carries the host's free disk; a platform from before it
// refuses the field (400, unknown field), and the claim goes again at once
// without it, as do the client's later claims, so the host keeps taking work
// while the platform is older than agentd.
func TestAClaimCarriesTheFreeDiskAnOldPlatformIsSpared(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	old := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		refuse := old && b["disk_free_bytes"] != nil
		mu.Unlock()
		if refuse {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"disk_free_bytes\""}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	disk := &agentd.Disk{Host: 3 << 30, Root: 2 << 30}
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	if cl, err := c.ClaimWith(ctx, 1, []agentd.Tool{cursor}, disk); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	if b := bodies[0]; b["disk_free_bytes"] != float64(3<<30) || b["root_free_bytes"] != float64(2<<30) {
		t.Fatalf("the claim's body %v", b)
	}

	old = true
	o := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	for i := 0; i < 2; i++ {
		if cl, err := o.ClaimWith(ctx, 1, []agentd.Tool{cursor}, disk); err != nil || cl != nil {
			t.Fatalf("claim %d to an old platform: %+v %v", i, cl, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 4 || bodies[1]["disk_free_bytes"] == nil || bodies[2]["disk_free_bytes"] != nil || bodies[3]["disk_free_bytes"] != nil {
		t.Fatalf("to an old platform: %v", bodies[1:])
	}
}

// TD-415: once an attempt is settled (its end is the platform's and its
// group is gone) its workspace is emptied and its output removed, so a host
// does not keep every attempt's checkout, modules and builds; the
// workspace's directory stays, so the same attempt is still never run twice
// (rule R2). The daemon is built as before TD-415 (its workspaces kept unless
// said otherwise), so this fails on the code before it.
func TestASettledAttemptsWorkspaceGoes(t *testing.T) {
	_, _, base := platform(t)
	root := t.TempDir()
	reg, err := agentd.OpenRegistry(filepath.Join(root, "registry.json"))
	must(t, err)
	d := &agentd.Daemon{
		Client: &agentd.Client{Base: base, Host: "h1", Token: "fixture-agentd-h1"}, Registry: reg, Root: root, Self: os.Args[0],
		Executors: map[string]agentd.Executor{"exec:cursor": script(`mkdir -p build && head -c 4096 /dev/zero > build/blob; printf '# done\n' > "$OUT/report.md"`)},
		Remote:    func(string) string { return "" },
		Beat:      100 * time.Millisecond, Retry: 50 * time.Millisecond, SalvageEvery: 50 * time.Millisecond, KillGrace: time.Second, Logf: t.Logf,
	}
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if err != nil || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	dir := agentd.WorkspaceDir(root, cl.TaskID, cl.AttemptNo)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the settled attempt's workspace holds %d entries (%v)", len(entries), err)
	}
	if _, err = os.Stat(dir + ".out"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the settled attempt's output is there: %v", err)
	}
	if _, err = d.Run(ctx, cl); !errors.Is(err, agentd.ErrWorkspaceExists) {
		t.Fatalf("the same attempt ran twice: %v", err)
	}
}
