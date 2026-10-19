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

// platformTakes fails t when the platform's scans of an upload (H8, T11)
// refuse got as a kind.
func platformTakes(t *testing.T, kind string, got []byte) {
	t.Helper()
	if p, found := store.LocalPath(kind, got); found {
		t.Errorf("as a %s the platform refuses it for a local path: %q", kind, p)
	}
	if ref, found := store.WorkspaceRef(kind, got); found {
		t.Errorf("as a %s the platform refuses it for a workspace: %q", kind, ref)
	}
}

// Review 380 A1: a report that names its own checkout in a form the rewrite
// does not know (a doubled slash, "/./", a JSON escape) is still handed in as
// the platform takes it: such a line is withheld, the rest and the verdict
// kept, and a JSON report is withheld as its one line.
func TestHandTextWithholdsWhatTheScansStillRefuse(t *testing.T) {
	const tid = "t_01M5B2WKM1CYDFE4PWGE4D3D42"
	root := "/home/bifrost-agentd-stg/agent-work/agentd"
	dir := WorkspaceDir(root, tid, 1)
	a := &Attempt{Dir: dir, Out: dir + ".out"}
	body := []byte(strings.Join([]string{"# Review", "kept: " + dir + "/api/y.go",
		root + "/tasks//" + tid + "/a1/api/x.go", "also " + root + "/tasks/./" + tid + "/a1/api/x.go", VerdictPass}, "\n") + "\n")
	for _, kind := range []string{"report", "review"} {
		got := a.handText(kind, body)
		platformTakes(t, kind, got)
		if !bytes.Contains(got, []byte("kept: ./api/y.go\n")) || bytes.Count(got, []byte("(withheld by agentd")) != 2 || reviewVerdict(got) != "done" {
			t.Errorf("%s handed in as:\n%s", kind, got)
		}
	}
	escaped := []byte(`{"text":"read\n` + strings.ReplaceAll(dir+"/api/x.go", "/", `\/`) + `"}`)
	got := a.handText("report", escaped)
	platformTakes(t, "report", got)
	if bytes.Contains(got, []byte(tid)) {
		t.Errorf("a JSON report handed in as:\n%s", got)
	}
}

// Review 380 A2: a report within the platform's 1 MiB that masking makes
// longer is cut at a line to fit, and keeps its verdict; one without a line
// to cut at is cut where it must be.
func TestHandTextKeepsThePlatformsLimit(t *testing.T) {
	dir := WorkspaceDir("/home/bifrost-agentd/agent-work/agentd", "t_01M5B2WKM1CYDFE4PWGE4D3D42", 1)
	a := &Attempt{Dir: dir, Out: dir + ".out"}
	line := strings.Repeat("~/", 40) + "\n"
	lines := []byte(strings.Repeat(line, (maxBody-len(VerdictPass)-1)/len(line)) + VerdictPass + "\n")
	oneLine := []byte(strings.Repeat("/tmp/", (maxBody-len(VerdictFail)-1)/5) + "\n" + VerdictFail)
	for _, c := range []struct {
		body    []byte
		verdict string
	}{{lines, "done"}, {oneLine, "failed"}} {
		if len(c.body) > maxBody {
			t.Fatalf("the report is %d bytes, over the limit before masking", len(c.body))
		}
		for _, kind := range []string{"report", "review"} {
			got := a.handText(kind, c.body)
			platformTakes(t, kind, got)
			if len(got) > maxBody || !bytes.Contains(got, []byte("(cut by agentd")) || reviewVerdict(got) != c.verdict {
				t.Errorf("%s of %d bytes handed in as %d bytes, verdict %q, ending %q", kind, len(c.body), len(got), reviewVerdict(got), got[max(0, len(got)-120):])
			}
		}
	}
	if got := a.handText("report", lines); !bytes.HasSuffix(got, []byte("<~>/\n(cut by agentd: the report was over 1 MiB once masked)\n"+VerdictPass+"\n")) {
		t.Errorf("not cut at a line: %q", got[len(got)-160:])
	}
}

// STG walk 3: a report whose paths agentd rewrote says so in its first line,
// so quoted code is not read as the code; a report with nothing rewritten,
// and a log tail, carry no such line. The verdict stays the last line.
func TestHandTextSaysWhatItRewrote(t *testing.T) {
	const tid = "t_01M5B2WKM1CYDFE4PWGE4D3D42"
	dir := WorkspaceDir("/home/bifrost-agentd-stg/agent-work/agentd", tid, 1)
	a := &Attempt{Claim: &Claimed{TaskID: tid, Role: "review"}, Dir: dir, Out: dir + ".out"}
	quoted := []byte("the pattern is `/Users/|/home/|/tmp/`\n" + VerdictPass + "\n")
	got := a.handText("review", quoted)
	if !bytes.HasPrefix(got, []byte(rewrittenNote)) || !bytes.HasSuffix(got, []byte(VerdictPass+"\n")) || reviewVerdict(got) != "done" {
		t.Fatalf("a rewritten review:\n%s", got)
	}
	if p, found := store.LocalPath("review", got); found {
		t.Fatalf("the note names a local path %q", p)
	}
	plain := []byte("all tests pass\n" + VerdictPass + "\n")
	if got = a.handText("review", plain); !bytes.Equal(got, plain) {
		t.Fatalf("a review with no path was changed:\n%s", got)
	}
	if got = a.handText("log_tail", quoted); bytes.Contains(got, []byte(rewrittenNote)) {
		t.Fatal("a log tail got the note")
	}
}

// Review 405 A2: a report the note pushes over the limit, with a verdict
// line a terminal coloured, still ends on its verdict.
func TestHandTextKeepsAColouredVerdictWhenCut(t *testing.T) {
	const tid = "t_01M5B2WKM1CYDFE4PWGE4D3D42"
	dir := WorkspaceDir("/home/bifrost-agentd-stg/agent-work/agentd", tid, 1)
	a := &Attempt{Claim: &Claimed{TaskID: tid, Role: "review"}, Dir: dir, Out: dir + ".out"}
	for verdict, want := range map[string]string{VerdictPass: "done", VerdictFail: "failed"} {
		raw := []byte(strings.Repeat("x\n", (maxBody-100)/2) + "/tmp/x\n\x1b[32m" + verdict + "\x1b[0m\n")
		got := a.handText("review", raw)
		if len(got) > maxBody || reviewVerdict(got) != want || !bytes.HasSuffix(got, []byte(verdict+"\n")) {
			t.Fatalf("%s: %d bytes, verdict %q, ends %q", verdict, len(got), reviewVerdict(got), got[len(got)-60:])
		}
	}
}

// R2-2 (STG walks 2 and 3): the prompt says Go's caches are set, to keep
// them, unless the task asks for a fresh one (review 405 B1).
func TestPromptKeepsTheCaches(t *testing.T) {
	p := Prompt(&Attempt{Spec: TaskSpec{Text: "do it"}})
	if !strings.Contains(p, "GOMODCACHE") || !strings.Contains(p, "keep them as they are") || !strings.Contains(p, "unless the task asks for a fresh cache") {
		t.Fatalf("prompt:\n%s", p)
	}
}
