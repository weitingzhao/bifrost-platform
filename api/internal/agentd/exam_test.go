package agentd_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// examPlatform hands out one paper and takes its answers a part at a time,
// as the platform's exam routes do (agentrtapi/exams.go): 409 while the
// paper is open, 422 for a run without its pins or an answer its scans
// refuse (H8, T11, on the answer as JSON), 409 for a run answered before,
// and graded once every run has an answer.
type examPlatform struct {
	mu     sync.Mutex
	paper  store.ExamPaper
	gets   int
	issued bool
	parts  []store.ExamSubmission
	have   map[string]store.ExamAnswer
}

func newExamPlatform(t *testing.T, items ...store.ExamPaperItem) (*examPlatform, *httptest.Server) {
	t.Helper()
	now := time.Now().UTC()
	f := &examPlatform{have: map[string]store.ExamAnswer{}, paper: store.ExamPaper{RunID: strings.Repeat("5e", 32), Job: "chief_of_staff",
		SetSHA256: strings.Repeat("ab", 32), Host: "h1", Executor: "exec:claude", Nonce: "n", IssuedAt: now, T0: now.Truncate(time.Minute),
		ExpiresAt: now.Add(store.ExamPaperLife), Items: items}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *examPlatform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	refuse := func(code int, kind, msg string) { reply(code, map[string]string{"error": kind, "message": msg}) }
	if r.Header.Get("Authorization") != "Bearer fixture-agentd-h1" {
		refuse(http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/exams/chief_of_staff":
		f.gets++
		if q := r.URL.Query(); q.Get("host_id") != "h1" || q.Get("executor") != "exec:claude" {
			refuse(http.StatusUnprocessableEntity, "invalid", "not this host's paper")
			return
		}
		if f.issued {
			refuse(http.StatusConflict, "state", "store: exec:claude has paper "+f.paper.RunID+" open")
			return
		}
		f.issued = true
		reply(http.StatusOK, f.paper)
	case r.Method == http.MethodPost && r.URL.Path == "/exams":
		var body struct {
			HostID string `json:"host_id"`
			store.ExamSubmission
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.HostID != "h1" {
			refuse(http.StatusBadRequest, "bad_request", fmt.Sprint(err))
			return
		}
		s := body.ExamSubmission
		switch {
		case !s.PinsOK:
			refuse(http.StatusUnprocessableEntity, "invalid", "pins_ok is not true")
			return
		case s.RunID != f.paper.RunID || s.ExamSHA256 != f.paper.SetSHA256 || s.Executor != "exec:claude" || s.Provider != "anthropic" ||
			s.ToolVersion == "" || len(s.Answers) != 1:
			refuse(http.StatusUnprocessableEntity, "invalid", "not a part of this paper")
			return
		}
		for _, a := range s.Answers {
			doc, _ := json.Marshal(a)
			if lp, found := localpath.Find(doc); found {
				refuse(http.StatusUnprocessableEntity, "invalid", "the answer holds a local path: "+lp)
				return
			}
			if ref, found := localpath.WorkspaceRef(doc); found {
				refuse(http.StatusUnprocessableEntity, "invalid", "the answer refers to a task's workspace: "+ref)
				return
			}
			if _, had := f.have[fmt.Sprintf("%s/%d", a.Item, a.RunNo)]; had {
				refuse(http.StatusConflict, "key_conflict", "answered before with another answer")
				return
			}
		}
		for _, a := range s.Answers {
			f.have[fmt.Sprintf("%s/%d", a.Item, a.RunNo)] = a
		}
		f.parts = append(f.parts, s)
		total := 0
		for _, it := range f.paper.Items {
			total += len(it.Runs)
		}
		reply(http.StatusOK, map[string]any{"run_id": s.RunID, "graded": len(f.have) == total, "answered": len(f.have), "expected": total,
			"event_id": 9, "rows": 1})
	default:
		refuse(http.StatusNotFound, "not_found", r.URL.Path)
	}
}

func (f *examPlatform) answers() map[string]store.ExamAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]store.ExamAnswer{}
	for k, v := range f.have {
		out[k] = v
	}
	return out
}

func briefItem(id string, runs ...int) store.ExamPaperItem {
	if len(runs) == 0 {
		runs = []int{1, 2, 3}
	}
	return store.ExamPaperItem{Item: id, T0: time.Now().UTC(), Runs: runs, Rounds: 1,
		Spec: json.RawMessage(`{"text":"# Answer a message\n\nItem ` + id + `."}`)}
}

func planItem(id, repo, base string) store.ExamPaperItem {
	return store.ExamPaperItem{Item: id, T0: time.Now().UTC(), Runs: []int{1, 2, 3}, Rounds: 2, Repo: repo, BaseCommit: base,
		Spec: json.RawMessage(`{"text":"# Plan the goal\n\nItem ` + id + `."}`)}
}

// claudeRun is one attempt the fake claude was asked for.
type claudeRun struct {
	item, dir, out, text, config string
	no                           int
}

// fakeClaude is the production claude executor (its environment, its own
// configuration directory, its report read from the stream) running a shell
// in place of claude: script gives the n-th attempt's stream-json events
// (in $STREAM) and its shell, `cat "$STREAM"` when none; $EXAMROOT is where
// the papers are kept.
func fakeClaude(t *testing.T, root string, script func(a *agentd.Attempt, n int) ([]any, string)) (agentd.Executor, func() []claudeRun) {
	t.Helper()
	claude := agentd.Executors(os.Args[0], os.Environ(), agentd.Secrets{})["exec:claude"]
	var mu sync.Mutex
	var runs []claudeRun
	ex := func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := claude(a)
		if err != nil {
			return sp, err
		}
		cfg := ""
		for _, kv := range sp.Env {
			if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
				cfg = v
			}
		}
		_, rest, _ := strings.Cut(a.Spec.Text, "Item ")
		item, _, _ := strings.Cut(rest, ".")
		mu.Lock()
		n := len(runs)
		runs = append(runs, claudeRun{item: item, dir: a.Dir, out: a.Out, text: a.Spec.Text, config: cfg, no: a.Claim.AttemptNo})
		mu.Unlock()
		events, sh := script(a, n)
		var b strings.Builder
		for _, ev := range events {
			line, merr := json.Marshal(ev)
			if merr != nil {
				return sp, merr
			}
			b.Write(append(line, '\n'))
		}
		stream := filepath.Join(t.TempDir(), "stream")
		if err = os.WriteFile(stream, []byte(b.String()), 0o600); err != nil {
			return sp, err
		}
		if sh == "" {
			sh = `cat "$STREAM"`
		}
		sp.Argv = []string{"sh", "-c", sh}
		sp.Env = append(sp.Env, "AGENTD_TEST_LAUNCHER=1", "STREAM="+stream, "EXAMROOT="+filepath.Join(root, "exam"))
		return sp, nil
	}
	return ex, func() []claudeRun {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(runs)
	}
}

func initEv(model string) map[string]any {
	return map[string]any{"type": "system", "subtype": "init", "model": model, "tools": []string{"Read", "Grep", "Glob"}}
}

func toolEv(tool string, input map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": tool, "input": input}}}}
}

func resultEv(text string) map[string]any { return map[string]any{"type": "result", "result": text} }

// newExam is an exam of this host h1 on srv, its runs pinned as the wall
// would pin them.
func newExam(t *testing.T, root string, srv *httptest.Server, ex agentd.Executor) *agentd.Exam {
	t.Helper()
	e := &agentd.Exam{Root: root, Self: os.Args[0], Client: &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd-h1"},
		Executor: ex, Tool: agentd.Tool{Executor: "exec:claude", Provider: "anthropic", Model: "opus"}, ToolVersion: "2.1.0 (Claude Code)",
		Limit: 20 * time.Second, KillGrace: time.Second, Retry: 10 * time.Millisecond, Logf: t.Logf}
	e.ConfineForTest(func(_ *agentd.Attempt, sp agentd.Spawn) (agentd.Spawn, string) { return sp, "" })
	return e
}

// noAnswerKept: nothing under root holds text only an answer has, and no
// task's directory is left.
func noAnswerKept(t *testing.T, root, answer string) {
	t.Helper()
	must(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(b), answer) {
			t.Errorf("%s keeps an answer", p)
		}
		return rerr
	}))
	if entries, err := os.ReadDir(filepath.Join(root, "tasks")); err == nil && len(entries) > 0 {
		t.Errorf("a run left %s in tasks", entries[0].Name())
	}
}

// The chief exam's S4 (the cut §3.1, §5): each run is sat as an attempt of
// the production executor in a working task's directories of its own, with
// a configuration directory of its own, and handed in at once, alone, its
// answer masked; a sitting cut short goes on with the paper it kept, and
// hands in only what it had not; once the paper is graded nothing of it is
// kept, and no answer is ever kept.
func TestExamGoesOnWhereItStopped(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1"), briefItem("B2"))
	stopped, cut := context.WithCancel(context.Background())
	defer cut()
	answer := func(a *agentd.Attempt, n int) ([]any, string) {
		return []any{initEv("claude-test-5"), toolEv("Read", map[string]any{"file_path": filepath.Join(a.Dir, "notes.md")}),
			toolEv("Glob", map[string]any{"pattern": "**/*.md"}), resultEv(fmt.Sprintf("Seat reply %d, from %s/notes.md.", n, a.Dir))}, ""
	}
	ex, runs := fakeClaude(t, root, func(a *agentd.Attempt, n int) ([]any, string) {
		if n == 2 {
			// The third run is cut short, as a SIGTERM would.
			time.AfterFunc(300*time.Millisecond, cut)
			return nil, "sleep 30"
		}
		return answer(a, n)
	})
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(stopped, "chief_of_staff")
	must(t, err)
	if err = e.Sit(stopped, p, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a sitting cut short: %v", err)
	}
	if len(f.parts) != 2 || len(runs()) != 3 {
		t.Fatalf("%d parts handed in, %d runs sat", len(f.parts), len(runs()))
	}
	noAnswerKept(t, root, "Seat reply ")

	ex2, runs2 := fakeClaude(t, root, answer)
	e2 := newExam(t, root, srv, ex2)
	p2, err := e2.Paper(ctx, "chief_of_staff")
	must(t, err)
	if p2.RunID != p.RunID || f.gets != 1 {
		t.Fatalf("went on with paper %s after %d papers asked for", p2.RunID, f.gets)
	}
	must(t, e2.Sit(ctx, p2, nil))
	got := []string{}
	for _, r := range runs2() {
		got = append(got, r.item)
	}
	if fmt.Sprint(got) != "[B1 B2 B2 B2]" || len(f.parts) != 6 {
		t.Fatalf("went on with %v, %d parts", got, len(f.parts))
	}
	for _, s := range f.parts {
		a := s.Answers[0]
		if !s.PinsOK || s.Model != "claude-test-5" || s.ToolVersion != "2.1.0 (Claude Code)" || a.Round != 1 || a.Conclusion != "done" ||
			a.OutOfWorkspace || !strings.HasPrefix(a.Text, agentd.RewrittenNote+"Seat reply ") || !strings.HasSuffix(a.Text, " from ./notes.md.") {
			t.Fatalf("a part: %+v", s)
		}
	}
	// Every round had a workspace, an output and a configuration directory
	// of its own, like a working task's.
	dirs := map[string]bool{}
	for _, r := range append(runs(), runs2()...) {
		if dirs[r.dir] || dirs[r.config] || r.config != filepath.Join(r.out, "home", ".claude") ||
			filepath.Base(filepath.Dir(filepath.Dir(r.dir))) != "tasks" || !strings.HasPrefix(filepath.Base(filepath.Dir(r.dir)), "t_") {
			t.Fatalf("run %+v shares or is not a working task's", r)
		}
		dirs[r.dir], dirs[r.config] = true, true
	}
	if _, err = os.Stat(filepath.Join(root, "exam", "chief_of_staff-claude")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the graded paper is kept: %v", err)
	}
	noAnswerKept(t, root, "Seat reply ")
}

// A plan the controller would not use goes back once, in a new session with
// the controller's own rework text (store.ReworkText); a plan it would use
// does not. The checkout is one commit holding the item's tree, the clone
// gone.
func TestExamPlanGoesBackOnce(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, base := bareRemote(t, env)
	root := t.TempDir()
	f, srv := newExamPlatform(t, planItem("P1", "bifrost-platform", base), planItem("P2", "bifrost-platform", base))
	good := "The plan.\n\n```json\n" + `{"mission": {}, "tasks": [{"ref": "t1", "title": "Do it", "repo": "bifrost-platform", "text": "Do it."}]}` + "\n```\n"
	ex, runs := fakeClaude(t, root, func(a *agentd.Attempt, _ int) ([]any, string) {
		sh := `[ "$(git rev-list --all | wc -l | tr -d ' ')" = 1 ] && [ -f README.md ] && [ ! -e ../src ] && cat "$STREAM"`
		if strings.Contains(a.Spec.Text, "Item P1.") && a.Claim.AttemptNo == 1 {
			return []any{initEv("claude-test-5"), resultEv("No plan, sorry.")}, sh
		}
		return []any{initEv("claude-test-5"), resultEv(good)}, sh
	})
	e := newExam(t, root, srv, ex)
	e.Remote, e.GitEnv = func(string) string { return bare }, env
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, e.Sit(ctx, p, nil))
	rs := runs()
	if len(rs) != 9 {
		t.Fatalf("%d rounds sat, want P1 3 × 2 and P2 3 × 1", len(rs))
	}
	for i, r := range rs {
		if r.item == "P1" && r.no == 2 && r.text != store.ReworkText(rs[i-1].text, 2, "the report has no ```json block") {
			t.Fatalf("round 2 read:\n%s", r.text)
		}
	}
	for k, a := range f.answers() {
		want := map[string]int{"P1": 2, "P2": 1}[strings.Split(k, "/")[0]]
		if a.Round != want || a.Text != good || a.Conclusion != "done" {
			t.Fatalf("%s: round %d, %s: %q", k, a.Round, a.Conclusion, a.Text)
		}
	}
}

// A paper whose commit carries the exam is not sat: no run starts, nothing
// is handed in, nothing is left.
func TestExamRefusesATreeWithTheExam(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, _ := bareRemote(t, env)
	seed := filepath.Join(t.TempDir(), "seed")
	sh(t, filepath.Dir(seed), env, "git", "clone", "--quiet", bare, seed)
	must(t, os.MkdirAll(filepath.Join(seed, "agent-config", "Chief-Exam"), 0o700))
	must(t, os.WriteFile(filepath.Join(seed, "agent-config", "Chief-Exam", "q.json"), []byte("{}\n"), 0o600))
	sh(t, seed, env, "git", "add", "agent-config")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "with the exam")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	withExam := sh(t, seed, env, "git", "rev-parse", "HEAD")
	root := t.TempDir()
	f, srv := newExamPlatform(t, planItem("P1", "bifrost-trade-infra", withExam))
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) { return []any{resultEv("x")}, "" })
	e := newExam(t, root, srv, ex)
	e.Remote, e.GitEnv = func(string) string { return bare }, env
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "agent-config/Chief-Exam") {
		t.Fatalf("a tree with the exam: %v", err)
	}
	if len(runs()) != 0 || len(f.parts) != 0 {
		t.Fatalf("%d runs started, %d parts handed in", len(runs()), len(f.parts))
	}
	noAnswerKept(t, root, "with the exam")
}

// A seat that reads outside its workspace (a path, a pattern that climbs
// out, a canary in its home or beside the papers) is sat once more, and
// handed in as out of its workspace when it strays again.
func TestExamOutOfWorkspace(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("O1", 1), briefItem("O2", 1), briefItem("O3", 1), briefItem("O4", 1), briefItem("O5", 1))
	tries := map[string]int{}
	ex, runs := fakeClaude(t, root, func(a *agentd.Attempt, _ int) ([]any, string) {
		_, rest, _ := strings.Cut(a.Spec.Text, "Item ")
		item := rest[:2]
		tries[item]++
		reads := []any{initEv("claude-test-5"), toolEv("Read", map[string]any{"file_path": "notes.md"}),
			toolEv("Grep", map[string]any{"pattern": "x", "path": a.Dir, "glob": "*.go"})}
		switch {
		case item == "O1" && tries[item] == 1:
			reads = append(reads, toolEv("Read", map[string]any{"file_path": "/etc/hosts"}))
		case item == "O2":
			reads = append(reads, toolEv("Glob", map[string]any{"pattern": "../../**/*.md"}))
		case item == "O3":
			return nil, `printf '{"type":"result","result":"I read %s"}\n' "$(cat "$HOME"/canary-*.txt)"`
		case item == "O4":
			return nil, `printf '{"type":"result","result":"I read %s"}\n' "$(cat "$EXAMROOT"/canary-*.txt)"`
		}
		return append(reads, resultEv("Fine.")), ""
	})
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, e.Sit(ctx, p, nil))
	if len(runs()) != 9 {
		t.Fatalf("%d runs sat; want O1, O2, O3 and O4 twice, O5 once: %v", len(runs()), tries)
	}
	for k, want := range map[string]bool{"O1/1": false, "O2/1": true, "O3/1": true, "O4/1": true, "O5/1": false} {
		if a := f.answers()[k]; a.OutOfWorkspace != want {
			t.Errorf("%s: out of its workspace %v, want %v", k, a.OutOfWorkspace, want)
		}
	}
}

// The answer is handed in as the platform's scans take it, kept as JSON:
// this host's paths, its workspace and home by what they are to the seat,
// another task's workspace withheld, and a drive letter's colon before a
// line's end or a "<" kept apart from what the encoder writes after it.
func TestExamMasksTheAnswer(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("M1", 1))
	other := store.NewTaskID()
	ex, _ := fakeClaude(t, root, func(a *agentd.Attempt, _ int) ([]any, string) {
		text := fmt.Sprintf("Read %s/notes.md and %s/.claude.json.\nOption A:\nOn C:\\Users\\x, /home/someone/y and ~/k; <B:<z>\n"+
			"Look at /srv/agentd/tasks/%s/a1/main.go too.", a.Dir, filepath.Join(a.Out, "home"), other)
		return []any{initEv("claude-test-5"), resultEv(text)}, ""
	})
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, e.Sit(ctx, p, nil))
	got := f.answers()["M1/1"].Text
	for _, want := range []string{"Read ./notes.md and $HOME/.claude.json.", "Option A: \n", "<C: >\\Users", "<home>/someone", "<B: <z>",
		"(withheld by agentd"} {
		if !strings.Contains(got, want) {
			t.Errorf("the answer lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, other) {
		t.Fatalf("another task's workspace was handed in:\n%s", got)
	}
}

// Without its pins nothing is sat but a trial: a host without the wall asks
// for no paper; a trial sits the paper and hands nothing in, and a sitting
// with its pins goes on with that paper; a run whose pins are not all there
// does not start.
func TestExamWithoutPins(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1"))
	mark := filepath.Join(t.TempDir(), "started")
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) {
		return []any{initEv("claude-test-5"), resultEv("Trial answer.")}, `touch "` + mark + `"; cat "$STREAM"`
	})
	bare := &agentd.Exam{Root: root, Self: os.Args[0], Client: &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd-h1"},
		Executor: ex, Tool: agentd.Tool{Executor: "exec:claude", Provider: "anthropic"}, ToolVersion: "2.1.0", Logf: t.Logf}
	if _, err := bare.Paper(ctx, "chief_of_staff"); err == nil || !strings.Contains(err.Error(), "no write wall") || f.gets != 0 {
		t.Fatalf("a host without the wall: %v, %d papers asked for", err, f.gets)
	}
	bare.Tool.Executor = "exec:codex"
	if _, err := bare.Paper(ctx, "chief_of_staff"); err == nil || !strings.Contains(err.Error(), "only exec:claude") {
		t.Fatalf("codex sat an exam: %v", err)
	}
	bare.Tool.Executor, bare.Dev = "exec:claude", true
	p, err := bare.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, bare.Sit(ctx, p, nil))
	if len(runs()) != 3 || len(f.parts) != 0 {
		t.Fatalf("a trial: %d runs, %d parts handed in", len(runs()), len(f.parts))
	}
	if _, err = os.Stat(filepath.Join(root, "exam", "chief_of_staff-claude", "handed.jsonl")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a trial recorded runs: %v", err)
	}
	noAnswerKept(t, root, "Trial answer.")
	must(t, os.Remove(mark))
	unpinned := newExam(t, root, srv, ex)
	unpinned.ConfineForTest(func(_ *agentd.Attempt, sp agentd.Spawn) (agentd.Spawn, string) {
		return sp, "agentd: exec:claude is confined, but its command lacks: the wall over its own directories"
	})
	p, err = unpinned.Paper(ctx, "chief_of_staff")
	must(t, err)
	if err = unpinned.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "lacks: the wall") {
		t.Fatalf("a run without its pins: %v", err)
	}
	if _, serr := os.Stat(mark); !errors.Is(serr, fs.ErrNotExist) || len(f.parts) != 0 {
		t.Fatalf("a run without its pins started (%v) or was handed in (%d)", serr, len(f.parts))
	}
	pinned := newExam(t, root, srv, ex)
	p, err = pinned.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, pinned.Sit(ctx, p, []string{"B1"}))
	if len(f.parts) != 3 || f.gets != 1 {
		t.Fatalf("after the trial: %d parts, %d papers asked for", len(f.parts), f.gets)
	}
}

// What cannot go on is said, and nothing is sat: another paper open with
// none kept, an item the paper does not have; a run the platform took
// before (its record lost) is recorded and not sat again.
func TestExamRefusals(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1, 2))
	f.issued = true
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) { return []any{resultEv("Same.")}, "" })
	e := newExam(t, root, srv, ex)
	if _, err := e.Paper(ctx, "chief_of_staff"); !errors.Is(err, agentd.ErrState) || !strings.Contains(err.Error(), "keeps no paper") {
		t.Fatalf("another paper open: %v", err)
	}
	f.issued = false
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	if err = e.Sit(ctx, p, []string{"B9"}); err == nil || !strings.Contains(err.Error(), `no item "B9"`) || len(runs()) != 0 {
		t.Fatalf("an item the paper does not have: %v", err)
	}
	f.have["B1/1"] = store.ExamAnswer{Item: "B1", RunNo: 1}
	must(t, e.Sit(ctx, p, nil))
	if len(runs()) != 2 || len(f.parts) != 1 {
		t.Fatalf("%d runs, %d parts", len(runs()), len(f.parts))
	}
	if _, err = os.Stat(filepath.Join(root, "exam", "chief_of_staff-claude")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a paper every run of which is handed in is kept: %v", err)
	}
}

// Before anything is sat: what a run cut short left (its task, which the
// kept paper names) is removed; other work under tasks/ stops the sitting,
// since a seat could read it. A kept paper that lapsed is let go and a new
// one asked for.
func TestExamClearsWhatWasLeft(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1))
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) { return []any{resultEv("Fine.")}, "" })
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	dir := filepath.Join(root, "exam", "chief_of_staff-claude")
	left := store.NewTaskID()
	must(t, os.MkdirAll(filepath.Join(root, "tasks", left, "a1"), 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "sitting"), []byte(`{"task":"`+left+`"}`), 0o600))
	other := filepath.Join(root, "tasks", store.NewTaskID(), "a1")
	must(t, os.MkdirAll(other, 0o700))
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "holds 1 entries of other work") || len(runs()) != 0 {
		t.Fatalf("beside other work: %v, %d runs", err, len(runs()))
	}
	if _, err = os.Stat(filepath.Join(root, "tasks", left)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("what a run cut short left is there: %v", err)
	}
	must(t, os.RemoveAll(filepath.Dir(other)))
	// The paper lapses here, with a run of it cut short: the next sitting
	// ends what that run left before it lets the paper go, and asks for a
	// new one (review 415 B1).
	left = store.NewTaskID()
	must(t, os.MkdirAll(filepath.Join(root, "tasks", left, "a1"), 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "sitting"), []byte(`{"task":"`+left+`"}`), 0o600))
	p.ExpiresAt = time.Now().Add(-time.Minute)
	b, err := json.Marshal(p)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "paper.json"), b, 0o600))
	f.mu.Lock()
	f.issued = false
	f.mu.Unlock()
	p, err = e.Paper(ctx, "chief_of_staff")
	must(t, err)
	if f.gets != 2 || !p.ExpiresAt.After(time.Now()) {
		t.Fatalf("after a lapsed paper: %d papers asked for, %s", f.gets, p.ExpiresAt)
	}
	if _, err = os.Stat(filepath.Join(root, "tasks", left)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("what a run of the lapsed paper left is there: %v", err)
	}
	must(t, e.Sit(ctx, p, nil))
	if len(runs()) != 1 || len(f.parts) != 1 {
		t.Fatalf("%d runs, %d parts", len(runs()), len(f.parts))
	}
}

// A run whose executor leaves no answer of its own (a login, a vendor that
// failed) is not handed in: the sitting stops and the run is sat again. A
// run out of its time is the seat's: handed in, failed, with no text even
// when it said something before its time ran out.
func TestExamNoAnswerAndOutOfTime(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1), briefItem("B2", 1))
	failing := true
	ex, runs := fakeClaude(t, root, func(a *agentd.Attempt, _ int) ([]any, string) {
		if strings.Contains(a.Spec.Text, "Item B2.") {
			// It says something, then runs out of its time: nothing of it is
			// handed in (review 415 A1).
			return []any{initEv("claude-test-5"), resultEv("PASS, said before the time ran out.")}, `cat "$STREAM"; sleep 30`
		}
		if failing {
			return []any{initEv("claude-test-5"), map[string]any{"type": "result", "is_error": true, "result": "Failed to authenticate"}}, ""
		}
		return []any{initEv("claude-test-5"), resultEv("Fine.")}, ""
	})
	e := newExam(t, root, srv, ex)
	e.Limit = time.Second
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "left no answer") || len(f.parts) != 0 {
		t.Fatalf("no answer: %v, %d parts", err, len(f.parts))
	}
	failing = false
	must(t, e.Sit(ctx, p, nil))
	b2 := f.answers()["B2/1"]
	if len(runs()) != 3 || f.answers()["B1/1"].Conclusion != "done" || b2.Conclusion != "failed" || b2.Text != "" {
		t.Fatalf("%d runs; B2 handed in as %+v", len(runs()), b2)
	}
}

// A run's record that names a process group, as a sitting cut short while its
// executor ran leaves it (here written by hand, the group another shell's):
// the next sitting ends the group while its leader is the recorded one,
// before it removes the run's directories; a group it cannot show to be the
// run's keeps everything there and nothing is sat (review 415 A2). The
// executor's own end with its group left is TestExamKeepsARunWhoseGroupIsLeft;
// agentd's side is TestExamRunLeftBlocksTheDaemon.
func TestExamEndsWhatACutRunLeftRunning(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1))
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) { return []any{resultEv("Fine.")}, "" })
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	dir := filepath.Join(root, "exam", "chief_of_staff-claude")
	group := func(sh string) (int, string) {
		cmd := exec.Command("sh", "-c", sh)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		must(t, cmd.Start())
		go func() { _ = cmd.Wait() }()
		start, serr := agentd.StartTime(cmd.Process.Pid)
		must(t, serr)
		t.Cleanup(func() {
			if agentd.Alive(cmd.Process.Pid, start) {
				agentd.KillGroup(cmd.Process.Pid)
			}
		})
		return cmd.Process.Pid, start
	}
	sitting := func(task string, pgid int, start string) {
		must(t, os.MkdirAll(filepath.Join(root, "tasks", task, "a1"), 0o700))
		id, _ := agentd.ProcIdentity(pgid) // none once the leader is gone
		b, merr := json.Marshal(map[string]any{"task": task, "pgid": pgid, "start": start, "id": id})
		must(t, merr)
		must(t, os.WriteFile(filepath.Join(dir, "sitting"), b, 0o600))
	}

	// A record that does not read is no run's: nothing is sat.
	must(t, os.WriteFile(filepath.Join(dir, "sitting"), []byte("t_x\n"), 0o600))
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "not an exam run's record") || len(runs()) != 0 {
		t.Fatalf("a record that does not read: %v", err)
	}

	// Its leader gone, a process of the group left: not known to be the
	// run's, so nothing is killed, removed or sat.
	pgid, start := group("sleep 60 & exit 0")
	for deadline := time.Now().Add(10 * time.Second); agentd.Alive(pgid, start); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the shell did not exit")
		}
	}
	left := store.NewTaskID()
	sitting(left, pgid, start)
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "not known to be the run's. Do not end processes by that number alone") || len(runs()) != 0 {
		t.Fatalf("a group not known to be the run's: %v, %d runs", err, len(runs()))
	}
	if _, err = os.Stat(filepath.Join(root, "tasks", left)); err != nil || !agentd.GroupAlive(pgid) {
		t.Fatalf("the run's directories went (%v) or its group was ended", err)
	}
	agentd.KillGroup(pgid)
	for deadline := time.Now().Add(10 * time.Second); agentd.GroupAlive(pgid); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the group outlived KILL")
		}
	}
	must(t, os.RemoveAll(filepath.Join(root, "tasks", left)))

	// Its leader the recorded one: the group is ended, then the run's
	// directories go, then the run is sat.
	pgid, start = group("sleep 60")
	left = store.NewTaskID()
	sitting(left, pgid, start)
	must(t, e.Sit(ctx, p, nil))
	if agentd.GroupAlive(pgid) || len(runs()) != 1 || len(f.parts) != 1 {
		t.Fatalf("group alive %v, %d runs, %d parts", agentd.GroupAlive(pgid), len(runs()), len(f.parts))
	}
	if _, err = os.Stat(filepath.Join(root, "tasks", left)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("what the run cut short left is there: %v", err)
	}
}

// Review 417 A2: a run whose executor's group is still there after it ended
// (a process the KILL did not end yet) is not handed in, and its record and
// directories stay: nothing removes or overwrites what names that group; the
// next sitting ends it first, then sits the run.
func TestExamKeepsARunWhoseGroupIsLeft(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1))
	ex, runs := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) { return []any{resultEv("Fine.")}, "" })
	e := newExam(t, root, srv, ex)
	e.GroupAliveForTest(func(int) bool { return true })
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	if err = e.Sit(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "is not gone after it ended") || len(f.parts) != 0 {
		t.Fatalf("a group left: %v, %d parts", err, len(f.parts))
	}
	b, err := os.ReadFile(filepath.Join(root, "exam", "chief_of_staff-claude", "sitting"))
	must(t, err)
	var s struct {
		Task string `json:"task"`
		PGID int    `json:"pgid"`
	}
	must(t, json.Unmarshal(b, &s))
	if _, serr := os.Stat(filepath.Join(root, "tasks", s.Task)); serr != nil || s.PGID == 0 {
		t.Fatalf("the record %s, the run's directories: %v", b, serr)
	}
	e.GroupAliveForTest(agentd.GroupAlive)
	must(t, e.Sit(ctx, p, nil))
	if len(runs()) != 2 || len(f.parts) != 1 {
		t.Fatalf("%d runs, %d parts", len(runs()), len(f.parts))
	}
	noAnswerKept(t, root, "Fine.")
}

// Review 417 B1: an answer in a result event over 1 MiB, which the report is
// not taken from, is the seat's: handed in failed with a note, not taken for
// no answer (which would stop every sitting at that run).
func TestExamResultTooLong(t *testing.T) {
	root := t.TempDir()
	f, srv := newExamPlatform(t, briefItem("B1", 1))
	ex, _ := fakeClaude(t, root, func(*agentd.Attempt, int) ([]any, string) {
		return []any{initEv("claude-test-5"), map[string]any{"type": "result", "is_error": false, "result": strings.Repeat("x", 1<<20+1)}}, ""
	})
	e := newExam(t, root, srv, ex)
	p, err := e.Paper(ctx, "chief_of_staff")
	must(t, err)
	must(t, e.Sit(ctx, p, nil))
	a := f.answers()["B1/1"]
	if a.Conclusion != "failed" || !strings.HasPrefix(a.Text, "(not handed in by agentd: the answer was a result event of over ") {
		t.Fatalf("handed in as %s: %.200q", a.Conclusion, a.Text)
	}
}
