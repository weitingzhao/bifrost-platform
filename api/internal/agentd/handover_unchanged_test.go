package agentd

// TD-360: an implementation attempt that changed nothing concludes none and is
// never handed in (moved out of handover_test.go, which crossed code-health's
// 800-line limit).

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unchangedEntry is an implement attempt of effect branch at phase whose
// checkout, from the bare remote's base, the test leaves as it wants, with
// a report written.
func unchangedEntry(t *testing.T, d *Daemon, phase string) (Entry, *Attempt, *Workspace, string, []string) {
	t.Helper()
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	d.GitEnv, d.Remote = env, func(string) string { return bare }
	e, a := entryFor(t, d, "a1", phase)
	e.Effect, e.Repo, e.WorkID, e.PushedRef, e.Base, e.Executor = "branch", "r", "W-31", "refs/heads/agent/t_a1/a1", base, "exec:cursor"
	a.Claim.EffectClass, a.Claim.Repo, a.Claim.PushedRef = e.Effect, e.Repo, e.PushedRef
	ws, err := Prepare(bg, e.Workdir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(a.Out, "report.md"), []byte("# done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	return e, a, ws, bare, env
}

// handedNothing checks an unchanged attempt was ended and nothing of it was
// handed in: no persist, submit or salvage, nothing pushed, and the entry
// forgotten.
func handedNothing(t *testing.T, d *Daemon, f *fakePlatform, bare string, env []string, ref string) {
	t.Helper()
	for _, step := range []string{"persist", "submit", "salvage"} {
		if f.count(step) != 0 {
			t.Fatalf("%s sent for an unchanged attempt: %v", step, f.log())
		}
	}
	if f.count("ended") != 1 {
		t.Fatalf("not ended once: %v", f.log())
	}
	if head := run(t, t.TempDir(), env, "git", "ls-remote", bare, ref); head != "" {
		t.Fatalf("pushed to the attempt's branch: %s", head)
	}
	if got, ok := registered(t, d, "a1"); ok {
		t.Fatalf("still registered: %+v", got)
	}
}

func computedConclusions(t *testing.T, f *fakePlatform) []string {
	t.Helper()
	var got []string
	for _, b := range f.sent("computed") {
		var body struct {
			Conclusion string `json:"conclusion"`
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		got = append(got, body.Conclusion)
	}
	return got
}

// TD-360: an implementation that exited 0 with a report and changed
// nothing, or committed a change and reverted it itself (HEAD past the base,
// the tree the base's), concludes none and is only ended; the reason is in
// its log.
func TestDeliverEndsAnUnchangedImplementation(t *testing.T) {
	for _, reverted := range []bool{false, true} {
		f := newFake()
		d := fakeDaemon(t, f)
		e, a, ws, bare, env := unchangedEntry(t, d, "exited")
		if reverted {
			if err := os.WriteFile(filepath.Join(e.Workdir, "a.txt"), []byte("one\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			run(t, e.Workdir, env, "git", "add", "a.txt")
			run(t, e.Workdir, env, "git", "commit", "-q", "-m", "executor's own")
			run(t, e.Workdir, env, "git", "revert", "--no-edit", "HEAD")
			if head := run(t, e.Workdir, env, "git", "rev-parse", "HEAD"); head == e.Base {
				t.Fatal("HEAD is still the base")
			}
		}
		out, err := d.deliver(bg, a, ws, &e, nil)
		if err != nil || out.Submitted || out.Conclusion != "none" {
			t.Fatalf("reverted %v: deliver = %+v %v; calls %v", reverted, out, err, f.log())
		}
		if got := computedConclusions(t, f); !slices.Equal(got, []string{"none"}) {
			t.Fatalf("reverted %v: computed %v", reverted, got)
		}
		handedNothing(t, d, f, bare, env, e.PushedRef)
		if log, _ := os.ReadFile(filepath.Join(a.Out, "log.txt")); !strings.Contains(string(log), "the attempt changed nothing against its base "+e.Base) {
			t.Fatalf("reverted %v: the log does not say why:\n%s", reverted, log)
		}
	}
}

// TD-360: each step holds after a restart, from the journal: a computed
// that could not be sent leaves the entry unchanged with its conclusion
// none, and the takeover sends that and ends it; an entry already past
// computed with a done conclusion (a journal from before the check) is
// found unchanged when its commit is made, and ended, never handed the
// base.
func TestUnchangedHoldsAfterARestart(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, a, ws, bare, env := unchangedEntry(t, d, "exited")
	f.set("computed", always(http.StatusServiceUnavailable, "down"))
	if _, err := d.deliver(bg, a, ws, &e, nil); !errors.Is(err, ErrPending) {
		t.Fatalf("deliver = %v", err)
	}
	got, ok := registered(t, d, "a1")
	if !ok || got.Phase != "exited" || !got.Checked || !got.Unchanged || got.Conclusion != "none" {
		t.Fatalf("journal = %+v", got)
	}
	f.set("computed", nil)
	if err := d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if c := computedConclusions(t, f); len(c) < 2 || c[len(c)-1] != "none" {
		t.Fatalf("computed %v", c)
	}
	handedNothing(t, d, f, bare, env, e.PushedRef)

	f2 := newFake()
	d2 := fakeDaemon(t, f2)
	e2, _, _, bare2, env2 := unchangedEntry(t, d2, "computed")
	if err := d2.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if f2.count("computed") != 0 {
		t.Fatalf("computed sent again: %v", f2.log())
	}
	handedNothing(t, d2, f2, bare2, env2, e2.PushedRef)
}

// TD-360: an unchanged implementation whose lease is lost is neither
// pushed nor salvaged: it is ended.
func TestLostEndsAnUnchangedImplementation(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, a, ws, bare, env := unchangedEntry(t, d, "lost")
	e.EndHow = "lease_lost"
	out, err := d.lost(bg, a, ws, &e)
	if err != nil || out.Salvaged {
		t.Fatalf("lost = %+v %v; calls %v", out, err, f.log())
	}
	handedNothing(t, d, f, bare, env, e.PushedRef)
}
