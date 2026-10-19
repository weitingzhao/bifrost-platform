package agentd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-382 (STG M-22): a review whose report quotes paths, its own checkout's
// as a tool printed them, its task's sibling, its temporary directory and
// home, and the bare prefixes a review of the local-path rule names, is
// handed in as the platform takes it: the same scans the platform runs on an
// upload (store.LocalPath, store.WorkspaceRef) find nothing, the paths stay
// readable relative to the checkout, and the verdict line is the same. The
// log tail, at the end and at a checkpoint, is handed in the same way.
func TestPrepareHandsInAReportWithoutLocalPaths(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	const tid = "t_01M5B2WKM1CYDFE4PWGE4D3D42"
	dir := WorkspaceDir(d.Root, tid, 1)
	for _, p := range []string{dir, dir + ".out/home/.cursor", dir + ".out/tmp/go-build1", filepath.Join(filepath.Dir(dir), "bifrost-ui")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{AttemptID: "a_r1", TaskID: tid, AttemptNo: 1, Epoch: 1, Role: "review", Effect: "none", Workdir: dir, Phase: "computed",
		Group: GroupCleared, EndHow: "exit", Conclusion: "done"}
	if err = d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	a := &Attempt{Claim: &Claimed{TaskID: tid, AttemptID: e.AttemptID, AttemptNo: 1, Role: "review", Epoch: 1}, Dir: dir, Out: dir + ".out"}
	report := strings.Join([]string{
		"# Review",
		"- verify.json: `" + dir + "/api/internal/localpath/localpath.go:18` (and " + real + "/api/go.mod)",
		"- sibling: " + filepath.Dir(dir) + "/bifrost-ui/src/b.ts",
		"- temporary: " + dir + ".out/tmp/go-build1/x.o",
		"- home: " + dir + ".out/home/.cursor/cli-config.json",
		"- coloured: \x1b[4m" + dir + "/api/x.go\x1b[24m",
		"- the rule names `/Users/`, `/home/`, `/private/`, `/var/folders/`, `/tmp/`, `~/` and `C:\\`",
		"- error: open /tmp/go-build123/b001/exe: permission denied; /home/bifrost-agentd-stg/.local/bin/agent",
		VerdictPass,
	}, "\n") + "\n"
	if err = os.WriteFile(filepath.Join(a.Out, "report.md"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	logLine := `{"type":"result","result":"read ` + dir + `/api/x.go and /tmp/x"}` + "\n"
	if err = os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte(logLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = d.prepare(bg, a, nil, &e); err != nil || !e.Prepared || e.Report == "" {
		t.Fatalf("prepare = %v; %+v", err, e)
	}
	sent := f.sent("put:review")
	if len(sent) != 1 || e.Report != ArtifactID(sent[0]) {
		t.Fatalf("report uploads %d, report %s", len(sent), e.Report)
	}
	got := sent[0]
	for _, kind := range []string{"review", "report"} {
		if p, found := store.LocalPath(kind, got); found {
			t.Errorf("as a %s the platform refuses it for a local path: %q\n%s", kind, p, got)
		}
		if ref, found := store.WorkspaceRef(kind, got); found {
			t.Errorf("as a %s the platform refuses it for a workspace: %q\n%s", kind, ref, got)
		}
	}
	for _, want := range []string{"`./api/internal/localpath/localpath.go:18`", "(and ./api/go.mod)", "../bifrost-ui/src/b.ts",
		"$TMPDIR/go-build1/x.o", "$HOME/.cursor/cli-config.json", "coloured: ./api/x.go\n", "`<Users>/`", "`<tmp>/`", "`<C:>\\`",
		"open <tmp>/go-build123/b001/exe", "<home>/bifrost-agentd-stg/.local/bin/agent"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("the report handed in has no %q:\n%s", want, got)
		}
	}
	if reviewVerdict(got) != "done" {
		t.Errorf("the verdict did not survive:\n%s", got)
	}

	d.checkpoint(bg, a, nil, &e)
	tails := f.sent("put:log_tail")
	if len(tails) != 2 {
		t.Fatalf("log tail uploads = %d, want 2 (the end, a checkpoint)", len(tails))
	}
	for _, tail := range tails {
		if p, found := store.LocalPath("report", tail); found || !bytes.Contains(tail, []byte("read ./api/x.go and <tmp>/x")) {
			t.Errorf("log tail %q: local path %q", tail, p)
		}
	}
}

// TD-382 on the agent host's own layout (AGENTD_ROOT under each agent user's
// home, PROD and STG): what the M-22 review quoted, and what a review quotes
// of its own checkout, is taken, and a verify.json or a patch is handed in as
// it is.
func TestHandTextOnTheAgentHost(t *testing.T) {
	const tid = "t_01M5B2WKM1CYDFE4PWGE4D3D42"
	for _, root := range []string{"/home/bifrost-agentd/agent-work/agentd", "/home/bifrost-agentd-stg/agent-work/agentd"} {
		dir := WorkspaceDir(root, tid, 1)
		a := &Attempt{Claim: &Claimed{TaskID: tid, Role: "review"}, Dir: dir, Out: dir + ".out"}
		body := []byte(strings.Join([]string{
			"`localpath.pattern` matches `/Users/`、`/home/`、`/tmp/`.",
			dir + "/api/internal/localpath/localpath.go:18: var pattern",
			"go: " + dir + ".out/tmp/go/pkg/mod: permission denied",
			"socket " + cursorDataDir(root, a.Out) + "/projects/x.sock",
			"sibling " + root + "/tasks/" + tid + "/bifrost-ui/package.json",
			"home " + dir + ".out/home/.codex/config.toml",
			VerdictFail,
		}, "\n"))
		for _, kind := range []string{"report", "review"} {
			got := a.handText(kind, body)
			if p, found := store.LocalPath(kind, got); found {
				t.Errorf("%s, %s: a local path %q\n%s", root, kind, p, got)
			}
			if ref, found := store.WorkspaceRef(kind, got); found {
				t.Errorf("%s, %s: a workspace %q\n%s", root, kind, ref, got)
			}
			for _, want := range []string{"`<Users>/`、`<home>/`、`<tmp>/`", "./api/internal/localpath/localpath.go:18",
				"$TMPDIR/go/pkg/mod", "../bifrost-ui/package.json", "$HOME/.codex/config.toml"} {
				if !bytes.Contains(got, []byte(want)) {
					t.Errorf("%s, %s: no %q\n%s", root, kind, want, got)
				}
			}
			if reviewVerdict(got) != "failed" {
				t.Errorf("%s, %s: the verdict did not survive", root, kind)
			}
		}
		for _, kind := range []string{"verify", "checkpoint_patch"} {
			if got := a.handText(kind, body); !bytes.Equal(got, body) {
				t.Errorf("%s: a %s was changed", root, kind)
			}
		}
	}
}
