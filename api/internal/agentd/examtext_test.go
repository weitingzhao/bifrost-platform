package agentd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// What a tool call reads is read as the tool reads it: relative to the
// workspace, ~ as the home, a Grep's pattern as no path, a pattern's fixed
// start as where it searches, a link as where it leads now.
func TestExamReadsOutsideTheWorkspace(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "tasks", "t_x", "a1")
	away := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(ws, "sub"), 0o700))
	must(t, os.WriteFile(filepath.Join(away, "secret"), []byte("s\n"), 0o600))
	must(t, os.Symlink(away, filepath.Join(ws, "link")))
	for _, c := range []struct {
		tool   string
		in     map[string]any
		inside bool
	}{
		{"Read", map[string]any{"file_path": filepath.Join(ws, "a.go")}, true},
		{"Read", map[string]any{"file_path": "sub/a.go"}, true},
		{"Read", map[string]any{"file_path": ws + "/../a1.out/home/x"}, false},
		{"Read", map[string]any{"file_path": "~/x"}, false},
		{"Read", map[string]any{"file_path": filepath.Join(ws, "link", "secret")}, false},
		{"Read", map[string]any{"file_path": filepath.Join(ws, "link", "missing")}, true},
		{"Grep", map[string]any{"pattern": "/etc/passwd"}, true},
		{"Grep", map[string]any{"pattern": "x", "path": "/etc"}, false},
		{"Grep", map[string]any{"pattern": "x", "glob": "/etc/*.conf"}, false},
		{"Grep", map[string]any{"pattern": "x", "path": "sub", "glob": "*.go"}, true},
		{"Glob", map[string]any{"pattern": "**/*.go"}, true},
		{"Glob", map[string]any{"pattern": "/etc/*"}, false},
		{"Glob", map[string]any{"pattern": "../**"}, false},
		{"Glob", map[string]any{"pattern": "*.md", "path": filepath.Join(ws, "sub")}, true},
		{"Glob", map[string]any{"pattern": "~/**"}, false},
		{"NotebookRead", map[string]any{"notebook_path": "/tmp/x.ipynb"}, false},
	} {
		inside := true
		for _, p := range toolPaths(c.tool, c.in, ws) {
			inside = inside && within(ws, p)
		}
		if inside != c.inside {
			t.Errorf("%s %v: inside %v, paths %v", c.tool, c.in, inside, toolPaths(c.tool, c.in, ws))
		}
	}
	log := filepath.Join(t.TempDir(), "log.txt")
	use := func(name string, in map[string]any) string {
		b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "reading"}, map[string]any{"type": "tool_use", "name": name, "input": in}}}})
		return string(b)
	}
	must(t, os.WriteFile(log, []byte(strings.Join([]string{`{"type":"system","subtype":"init","model":"claude-test-5"}`, "not json",
		use("Read", map[string]any{"file_path": "a.go"}), `{"type":"user","message":{"content":[{"type":"tool_result","content":"/etc/hosts"}]}}`,
		use("Grep", map[string]any{"pattern": "x", "path": "/var/lib"}), use("Read", map[string]any{"file_path": "/etc/hosts"})}, "\n")), 0o600))
	model, outside, err := examReads(log, ws)
	if err != nil || model != "claude-test-5" || outside != "Grep /var/lib" {
		t.Fatalf("read the log as %q, %q, %v", model, outside, err)
	}
}

// An answer is handed in as the platform keeps it, in JSON, and scans it:
// a drive letter's colon before what the encoder escapes is kept apart from
// it; an answer too long as JSON is cut at a line; what the scans still find
// withholds it.
func TestExamText(t *testing.T) {
	for in, want := range map[string]string{
		"Option A:\nnext":  "Option A: \nnext",
		"A:\tx":            "A: \tx",
		`see Q:"x"`:        `see Q: "x"`,
		"<B:<x>, &C:&":     "<B: <x>, &C: &",
		"POLICY_ALERT: ok": "POLICY_ALERT: ok",
		"选项 A:\n":          "选项 A: \n",
		"A:\xffB":          "A:\uFFFDB",
	} {
		got := examText([]byte(in))
		doc, _ := json.Marshal(got)
		if got != want {
			t.Errorf("%q came out %q, want %q", in, got, want)
		}
		if lp, found := localpath.Find(doc); found {
			t.Errorf("%q as JSON still has %q", in, lp)
		}
	}
	if got := examText([]byte(`at C:\x`)); got != withheldReport {
		t.Errorf("a drive that is left is handed in: %q", got)
	}
	long := strings.Repeat("<<<<<<<<<<\n", 100000)
	got := examText([]byte(long))
	doc, _ := json.Marshal(got)
	if len(doc) > maxExamText || !strings.HasSuffix(got, truncatedNote) || !strings.HasPrefix(long, strings.TrimSuffix(got, truncatedNote)) ||
		!strings.HasSuffix(strings.TrimSuffix(got, truncatedNote), "\n") {
		t.Fatalf("a long answer: %d bytes as JSON, ends %q", len(doc), got[len(got)-80:])
	}
}

// A paper that is not this exam's, or whose items an executor cannot sit as
// a working task, is not sat.
func TestCheckPaper(t *testing.T) {
	now := time.Now()
	good := func() *store.ExamPaper {
		return &store.ExamPaper{RunID: strings.Repeat("a", 64), Job: "chief_of_staff", SetSHA256: strings.Repeat("b", 64), Host: "h1",
			Executor: "exec:claude", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Items: []store.ExamPaperItem{
				{Item: "B1", Runs: []int{1, 2, 3}, Rounds: 1, Spec: json.RawMessage(`{"text":"# a brief"}`)},
				{Item: "P1", Runs: []int{1, 2, 3}, Rounds: 2, Repo: "bifrost-trade-infra", BaseCommit: strings.Repeat("c", 40),
					Spec: json.RawMessage(`{"text":"# a plan"}`)}}}
	}
	if err := checkPaper(good(), "chief_of_staff", "exec:claude", "h1"); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(p *store.ExamPaper){
		"another host":            func(p *store.ExamPaper) { p.Host = "h2" },
		"another job":             func(p *store.ExamPaper) { p.Job = "implementer" },
		"a run id not a sha256":   func(p *store.ExamPaper) { p.RunID = "x" },
		"lapsed when issued":      func(p *store.ExamPaper) { p.ExpiresAt = p.IssuedAt },
		"no item":                 func(p *store.ExamPaper) { p.Items = nil },
		"an item twice":           func(p *store.ExamPaper) { p.Items = append(p.Items, p.Items[0]) },
		"an item that is no name": func(p *store.ExamPaper) { p.Items[0].Item = "../x" },
		"run 0":                   func(p *store.ExamPaper) { p.Items[0].Runs = []int{0} },
		"a run twice":             func(p *store.ExamPaper) { p.Items[0].Runs = []int{1, 1} },
		"no run":                  func(p *store.ExamPaper) { p.Items[0].Runs = nil },
		"three rounds":            func(p *store.ExamPaper) { p.Items[1].Rounds = 3 },
		"a repository, no commit": func(p *store.ExamPaper) { p.Items[1].BaseCommit = "" },
		"a short commit":          func(p *store.ExamPaper) { p.Items[1].BaseCommit = "abc" },
		"a repository path":       func(p *store.ExamPaper) { p.Items[1].Repo = "../x" },
		"checks":                  func(p *store.ExamPaper) { p.Items[0].Spec = json.RawMessage(`{"text":"x","verify":[["true"]]}`) },
		"setup":                   func(p *store.ExamPaper) { p.Items[0].Spec = json.RawMessage(`{"text":"x","setup":[["true"]]}`) },
		"siblings": func(p *store.ExamPaper) {
			p.Items[1].Spec = json.RawMessage(`{"text":"x","siblings":[{"repo":"bifrost-ui","ref":"main"}]}`)
		},
		"a check's spec": func(p *store.ExamPaper) { p.Items[0].Spec = json.RawMessage(`{"text":"x","check":"review"}`) },
		"no text":        func(p *store.ExamPaper) { p.Items[0].Spec = json.RawMessage(`{"text":"  "}`) },
		"no spec":        func(p *store.ExamPaper) { p.Items[0].Spec = json.RawMessage(`{"text":"x","unknown":1}`) },
	} {
		p := good()
		change(p)
		if err := checkPaper(p, "chief_of_staff", "exec:claude", "h1"); err == nil {
			t.Errorf("%s: sat", name)
		}
	}
}

// The tool's version is what claude says on the allowed PATH, run as the
// executor runs it: its own home and configuration directory, the updater
// off.
func TestToolVersion(t *testing.T) {
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+
		`[ -n "$CLAUDE_CONFIG_DIR" ] && [ "$HOME" = "$CLAUDE_CONFIG_DIR" ] && [ "$DISABLE_AUTOUPDATER" = 1 ] && echo '2.1.302 (Claude Code)' && echo more`+"\n"), 0o700))
	scratch := t.TempDir()
	v, err := ToolVersion(context.Background(), "exec:claude", []string{"PATH=" + bin + ":/bin:/usr/bin"}, scratch)
	if err != nil || v != "2.1.302 (Claude Code)" {
		t.Fatalf("version %q, %v", v, err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Fatalf("the version's home is left: %v", entries)
	}
	if _, err = ToolVersion(context.Background(), "exec:claude", []string{"PATH=relative:/nonexistent"}, scratch); err == nil {
		t.Fatal("a version with no claude on the PATH")
	}
	must(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o700))
	if _, err = ToolVersion(context.Background(), "exec:claude", []string{"PATH=" + bin}, scratch); err == nil {
		t.Fatal("a version of nothing")
	}
	if _, err = ToolVersion(context.Background(), "exec:codex", []string{"PATH=" + bin}, scratch); err == nil {
		t.Fatal("codex's version for an exam")
	}
}
