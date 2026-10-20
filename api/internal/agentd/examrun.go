package agentd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// One run of an exam's item (exam.go): its rounds, each an attempt of the
// production executor in a working task's directories, and what the seat
// read.

// sat is one run as it is handed in: the answer, the model the session ran,
// why the run's pins were not all there ("" when they were), and the first
// thing it read outside its workspace.
type sat struct {
	answer      store.ExamAnswer
	model, pins string
	outside     string
}

// examRound is one round of a run: the answer as handed in, the report as
// the platform keeps a report (ParsePlan reads it), how it ended and what
// the seat read.
type examRound struct {
	text, conclusion string
	report           []byte
	wallS            int
	model, pins      string
	outside          string
}

// sitRun sits run n of item: its rounds, and all of them once more when the
// seat read outside its workspace.
func (e *Exam) sitRun(ctx context.Context, dir string, it store.ExamPaperItem, spec TaskSpec, n int) (sat, error) {
	s, err := e.rounds(ctx, dir, it, spec, n)
	if err != nil || s.outside == "" {
		return s, err
	}
	e.logf("agentd exam: %s run %d read outside its workspace (%s): it is sat once more", it.Item, n, s.outside)
	if s, err = e.rounds(ctx, dir, it, spec, n); err == nil && s.outside != "" {
		e.logf("agentd exam: %s run %d read outside its workspace again (%s): handed in as such, not passed", it.Item, n, s.outside)
	}
	return s, err
}

// rounds sits a run's rounds as one task: the first, and a plan the
// controller would not use goes back once with why, as AcceptPlan sends it
// back (store.ReworkText).
func (e *Exam) rounds(ctx context.Context, dir string, it store.ExamPaperItem, spec TaskSpec, n int) (sat, error) {
	task := store.NewTaskID()
	// What a run cut short leaves is found again by its task (clearLeft).
	if err := os.WriteFile(filepath.Join(dir, "sitting"), []byte(task+"\n"), 0o600); err != nil {
		return sat{}, err
	}
	r, err := e.round(ctx, it, spec, task, 1)
	if err != nil {
		return sat{}, err
	}
	s := sat{model: r.model, pins: r.pins, outside: r.outside}
	round, wall := 1, r.wallS
	if why := planRefused(it, r); why != "" {
		e.logf("agentd exam: %s run %d: the controller would not use the plan (%s): round 2", it.Item, n, oneLine(why, 200))
		next := spec
		next.Text = store.ReworkText(spec.Text, 2, why)
		if r, err = e.round(ctx, it, next, task, 2); err != nil {
			return sat{}, err
		}
		round, wall, s.model = 2, wall+r.wallS, r.model
		s.pins, s.outside = firstSet(s.pins, r.pins), firstSet(s.outside, r.outside)
	}
	if err = os.Remove(filepath.Join(dir, "sitting")); err != nil {
		return sat{}, err
	}
	s.answer = store.ExamAnswer{Item: it.Item, RunNo: n, Round: round, Text: r.text, WallS: wall, Conclusion: r.conclusion,
		OutOfWorkspace: s.outside != ""}
	return s, nil
}

func firstSet(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// planRefused is why the controller would send a plan back, as AcceptPlan
// does before it looks at the mission: the attempt did not end done, or
// ParsePlan refuses the report as the platform keeps it, for the item's
// repository; "" when it would go on with it, or when the item has no
// second round (not a plan).
func planRefused(it store.ExamPaperItem, r examRound) string {
	if it.Rounds < 2 || it.Repo == "" {
		return ""
	}
	if r.conclusion != "done" {
		return fmt.Sprintf("the planner's attempt ended %q", r.conclusion)
	}
	var repos []string
	if _, ok := store.Repos[it.Repo]; ok {
		repos = append(repos, it.Repo)
	}
	if _, err := store.ParsePlan(r.report, repos); err != nil {
		return err.Error()
	}
	return ""
}

// round sits one round: a working task's directories, the executor walled
// and pinned, its answer and what it read; the directories go however it
// ends.
func (e *Exam) round(ctx context.Context, it store.ExamPaperItem, spec TaskSpec, task string, no int) (r examRound, err error) {
	role := "brief"
	if it.Repo != "" {
		role = "plan"
	}
	a := &Attempt{Claim: &Claimed{TaskID: task, AttemptNo: no, Role: role, Repo: it.Repo, BaseCommit: it.BaseCommit, EffectClass: "none",
		Tool: e.Tool}, Spec: spec, Dir: WorkspaceDir(e.Root, task, no), root: e.Root}
	a.Out = a.Dir + ".out"
	taskDir := filepath.Dir(a.Dir)
	defer func() {
		if rerr := forceRemove(taskDir); rerr != nil && err == nil {
			err = rerr
		}
	}()
	if err = os.MkdirAll(taskDir, 0o700); err != nil {
		return r, err
	}
	if it.Repo != "" {
		err = e.snapshot(ctx, it, a.Dir)
	} else {
		err = os.Mkdir(a.Dir, 0o700)
	}
	if err == nil {
		err = os.Mkdir(a.Out, 0o700)
	}
	if err != nil {
		return r, err
	}
	sp, err := e.Executor(a)
	if err != nil {
		return r, err
	}
	canaries, gone, err := e.canaries(a)
	defer gone()
	if err != nil {
		return r, err
	}
	sp, r.pins = e.confined(a, sp)
	if r.pins != "" && !e.Dev {
		return r, errors.New(r.pins)
	}
	log, err := os.OpenFile(filepath.Join(a.Out, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return r, err
	}
	sp.Stdout, sp.Stderr = log, log
	start := time.Now()
	code, timedOut, err := launchWithin(ctx, e.Self, sp, orDefault(e.Limit, 15*time.Minute), e.KillGrace)
	_ = log.Close()
	r.wallS = int(time.Since(start).Seconds())
	if err != nil {
		return r, err
	}
	afterExecutor(e.Tool.Executor, "", e.Root, a.Out, e.logf)
	var after error
	if sp.After != nil {
		after = sp.After()
	}
	report, rerr := readAtMost(filepath.Join(a.Out, "report.md"), maxBody)
	if rerr != nil || len(report) == 0 {
		report = nil
		if !timedOut {
			// No answer of the seat's own: a login or a vendor that failed is
			// not the seat's, and the run is sat again.
			return r, fmt.Errorf("the executor left no answer (exit %d): %v", code, errors.Join(after, rerr))
		}
	}
	r.conclusion = "done"
	if timedOut || code != 0 || report == nil {
		r.conclusion = "failed"
	}
	if r.model, r.outside, err = examReads(filepath.Join(a.Out, "log.txt"), a.Dir); err != nil {
		return r, err
	}
	if r.model == "" {
		r.model = e.Tool.Model
	}
	for _, c := range canaries {
		if r.outside == "" && bytes.Contains(report, []byte(c)) {
			r.outside = "a canary's content in its answer"
		}
	}
	if report != nil {
		r.report = a.handText("report", report)
		r.text = examText(r.report)
	}
	return r, nil
}

// confined is sp walled and pinned as a confined attempt of the executor
// runs at work (run.go), and why its pins are not all there ("" when they
// are).
func (e *Exam) confined(a *Attempt, sp Spawn) (Spawn, string) {
	if e.confine != nil {
		return e.confine(a, sp)
	}
	if why := e.unpinned(); why != "" {
		return sp, why
	}
	sp.Argv = append(wallArgv(e.Self, executorWrites(e.Tool.Executor, a, nil)), sp.Argv...)
	if err := checkPins(e.Wall, e.Tool.Executor, e.Self, a, nil, sp); err != nil {
		return sp, err.Error()
	}
	return sp, ""
}

// snapshot makes dir a repository of one commit holding the tree of the
// item's commit and none of its history (agentd bench's snapshot): cloned
// beside it, and the clone gone before anything runs. A tree with a
// chief-exam entry is refused: the exam would be there to read.
func (e *Exam) snapshot(ctx context.Context, it store.ExamPaperItem, dir string) error {
	// The clone goes before the executor starts; what an error leaves goes
	// with the task's directory (round).
	src := filepath.Join(filepath.Dir(dir), "src")
	ws, err := Prepare(ctx, src, e.Remote(it.Repo), it.BaseCommit, e.GitEnv)
	if err != nil {
		return fmt.Errorf("cloning %s: %w", it.Repo, err)
	}
	b := &Bench{GitEnv: e.GitEnv, CheckEnv: e.GitEnv}
	if err = b.snapshot(ctx, ws, it.BaseCommit, dir); err != nil {
		return err
	}
	if found, ferr := examEntry(dir); ferr != nil || found != "" {
		return errors.Join(ferr, fmt.Errorf("%s at %s has %s: the exam is in the tree, so no run of %s is sat", it.Repo, it.BaseCommit, found, it.Item))
	}
	if err = forceRemove(src); err != nil {
		return err
	}
	return b.initRepo(ctx, dir)
}

// examEntry is the first entry of tree named chief-exam, in any case, or "".
func examEntry(tree string) (string, error) {
	found := ""
	err := filepath.WalkDir(tree, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p != tree && strings.EqualFold(d.Name(), "chief-exam") {
			found, _ = filepath.Rel(tree, p)
			return filepath.SkipAll
		}
		return nil
	})
	return found, err
}

// canaries puts a file of random content in the attempt's home and one
// beside the kept papers, and returns their contents and how to remove the
// second (the first goes with the attempt's output).
func (e *Exam) canaries(a *Attempt) ([]string, func(), error) {
	gone := func() {}
	home := attemptHome(a.Out)
	if err := ownDir(home); err != nil {
		return nil, gone, err
	}
	var out []string
	for _, d := range []string{home, filepath.Join(e.Root, "exam")} {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, gone, err
		}
		c := hex.EncodeToString(b)
		path := filepath.Join(d, "canary-"+c[:8]+".txt")
		if err := os.WriteFile(path, []byte(c+"\n"), 0o600); err != nil {
			return nil, gone, err
		}
		if d != home {
			gone = func() { _ = os.Remove(path) }
		}
		out = append(out, c)
	}
	return out, gone, nil
}

// examReads reads an executor's stream-json log, all of it: the model its
// init event names, and the first path a tool call names outside dir ("" when
// none does).
func examReads(log, dir string) (model, outside string, err error) {
	f, err := openBounded(log, 1<<62, false)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()
	err = eachLine(f, maxBody, func(line []byte) {
		var ev map[string]any
		if json.Unmarshal(line, &ev) != nil {
			return
		}
		switch str(ev["type"]) {
		case "system":
			if m := str(ev["model"]); str(ev["subtype"]) == "init" && m != "" {
				model = m
			}
		case "assistant":
			content, _ := obj(ev["message"])["content"].([]any)
			for _, c := range content {
				b := obj(c)
				if str(b["type"]) != "tool_use" {
					continue
				}
				for _, p := range toolPaths(str(b["name"]), obj(b["input"]), dir) {
					if outside == "" && !within(dir, p) {
						outside = str(b["name"]) + " " + p
					}
				}
			}
		}
	})
	return model, outside, err
}

// toolPaths are the paths a tool call reads: the file a Read names, the
// directory a Grep or a Glob searches (none: the workspace), and the fixed
// start of a Glob's pattern or a Grep's glob, which may lead elsewhere; of
// another tool, what it names a path.
func toolPaths(name string, in map[string]any, dir string) []string {
	var out []string
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if s := str(in[k]); s != "" {
			out = append(out, s)
		}
	}
	pattern := ""
	switch name {
	case "Glob":
		pattern = str(in["pattern"])
	case "Grep":
		pattern = str(in["glob"])
	}
	if pattern != "" {
		base := firstOf(in, "path")
		if base == "" {
			base = dir
		}
		if !filepath.IsAbs(pattern) && !strings.HasPrefix(pattern, "~") {
			pattern = filepath.Join(base, pattern)
		}
		if i := strings.IndexAny(pattern, "*?[{"); i >= 0 {
			pattern = filepath.Dir(pattern[:i] + "x")
		}
		out = append(out, pattern)
	}
	return out
}

// within: p, a path a tool call names, is dir or beneath it, read as the
// tool reads it (relative to dir; ~ is the home, outside), and through the
// links it passes as they resolve now.
func within(dir, p string) bool {
	if strings.HasPrefix(p, "~") {
		return false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	p = filepath.Clean(p)
	roots := []string{filepath.Clean(dir)}
	if r := resolved(dir); r != "" {
		roots = append(roots, r)
	}
	under := func(p string) bool {
		for _, root := range roots {
			if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	if !under(p) {
		return false
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return under(r)
	}
	return true // not there: nothing was read
}

// escapedDrive is a drive letter's colon before a character the JSON
// encoder writes as an escape, which starts with a backslash: in the answer
// as the platform keeps it, H8 would read a drive there ("A:" at a line's
// end becomes "A:\n").
var escapedDrive = regexp.MustCompile(`((?:^|[^A-Za-z])[A-Za-z]:)([\x00-\x1f"<>&\x{2028}\x{2029}])`)

// maxExamText bounds an answer as JSON: the platform keeps it in a document
// of at most 1 MiB with the run's other fields.
const maxExamText = maxBody - 8<<10

// examText is a masked report as an answer is handed in. The platform keeps
// it in a JSON document and scans that (H8, T11): a space goes after a drive
// letter's colon the encoder would follow with an escape, an answer too long
// as JSON is cut at a line, and one the scans still refuse is withheld, as
// handText withholds a report.
func examText(masked []byte) string {
	s := strings.ToValidUTF8(string(masked), "\uFFFD")
	for range len(s) + 1 {
		next := escapedDrive.ReplaceAllString(s, "$1 $2")
		if next == s {
			break
		}
		s = next
	}
	doc, _ := json.Marshal(s)
	for len(doc) > maxExamText {
		keep := strings.ToValidUTF8(s[:len(s)*maxExamText/len(doc)-len(truncatedNote)-64], "")
		if i := strings.LastIndexByte(keep, '\n'); i > 0 {
			keep = keep[:i+1]
		}
		s = keep + truncatedNote
		doc, _ = json.Marshal(s)
	}
	_, local := localpath.Find(doc)
	_, names := localpath.WorkspaceRef(doc)
	if local || names {
		return withheldReport
	}
	return s
}

// ToolVersion is what the executor's program says it is (claude --version,
// its first line), run as the executor runs it: found on the allowed PATH,
// with the allowed environment, a home and configuration directory of its
// own under scratch (removed after) and the updater off.
func ToolVersion(ctx context.Context, executor string, environ []string, scratch string) (string, error) {
	if err := ExamExecutor(executor); err != nil {
		return "", err
	}
	env := AllowedEnv(environ)
	bin := binOnPath(env, "claude")
	if bin == "" {
		return "", errors.New("agentd: no claude on the allowed PATH")
	}
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return "", err
	}
	home, err := os.MkdirTemp(scratch, "version-")
	if err != nil {
		return "", err
	}
	defer func() { _ = forceRemove(home) }()
	vctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(vctx, bin, "--version")
	cmd.Dir = home
	cmd.Env = withEnv(withEnv(withEnv(env, "HOME", home), "CLAUDE_CONFIG_DIR", home), "DISABLE_AUTOUPDATER", "1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("agentd: %s --version: %w", bin, err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 100 || strings.ContainsFunc(line, unicode.IsControl) {
		return "", fmt.Errorf("agentd: %s --version says %q, not a version", bin, oneLine(line, 120))
	}
	return line, nil
}
