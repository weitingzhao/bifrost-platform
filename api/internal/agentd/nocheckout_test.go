package agentd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TD-421 (STG M-113): a branch claim with no repository or base commit ends
// before its executor starts, as prepare_failed, and its report says why.
// Without a checkout the executor would run in an empty directory, end "done"
// and hand in nothing the platform takes. A task with no effect has no checkout
// and still runs.
func TestABranchClaimWithoutACheckoutNeverStarts(t *testing.T) {
	for name, c := range map[string]struct{ effect, repo, base, says string }{
		"no base commit": {"branch", "bifrost-platform", "", "it names no base_commit"},
		"no repo":        {"branch", "", strings.Repeat("a", 40), "it names no repo"},
		"neither":        {"branch", "", "", "it names no repo and base_commit"},
		"no effect":      {"none", "", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			d := fakeDaemon(t, f)
			spec := []byte("# do it\n")
			f.artifacts[ArtifactID(spec)] = spec
			ran := filepath.Join(t.TempDir(), "ran")
			d.Executors = map[string]Executor{"exec:x": func(a *Attempt) (Spawn, error) {
				return Spawn{Argv: []string{"touch", ran}, Env: launcherEnv(), Dir: a.Dir}, nil
			}}
			cl := &Claimed{TaskID: "t1", AttemptID: "a1", AttemptNo: 1, Role: "implement", SpecArtifact: ArtifactID(spec),
				EffectClass: c.effect, Repo: c.repo, BaseCommit: c.base, Epoch: 1, Tool: Tool{Executor: "exec:x"}}
			out, err := d.Run(bg, cl)
			_, serr := os.Stat(ran)
			if c.effect == "none" {
				if errors.Is(err, ErrNoCheckout) || serr != nil {
					t.Fatalf("a task with no effect did not run: %+v %v", out, err)
				}
				return
			}
			if !errors.Is(err, ErrNoCheckout) || !strings.Contains(err.Error(), c.says) || out.EndHow != prepareEnd {
				t.Fatalf("run = %+v %v", out, err)
			}
			if serr == nil {
				t.Fatal("the executor ran")
			}
			ended := f.sent("ended")
			if len(ended) != 1 || !strings.Contains(string(ended[0]), `"end_how":"prepare_failed"`) {
				t.Fatalf("ended %q", ended)
			}
			said := false
			for id, body := range f.artifacts {
				if id != ArtifactID(spec) && strings.Contains(string(body), "needs a checkout") && strings.Contains(string(body), c.says) {
					said = true
				}
			}
			if !said {
				t.Fatal("no report handed in says why")
			}
		})
	}
}
