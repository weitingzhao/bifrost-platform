package agentd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// agentd exam (W-31 2.4 ④; the chief exam's cut, infra agent-config/work/
// multi-agent/CUT-chief-exam-2026-10-19.md §3.1, §5, §6 甲, the Owner
// 2026-10-19): a job's qualifying exam, sat on this host by one executor as
// it works, each answer handed to the platform, which grades the paper
// (store/exam.go). agentd never sees the exam's rules: it is handed a paper,
// each item's spec as the platform's own spec functions render it, and hands
// in text.
//
//  1. The paper: the one this host keeps for the job and the executor
//     (<root>/exam/<job>-<executor>/paper.json, with the runs handed in so
//     far, never their answers), or a new one from the platform, kept before
//     anything runs: an Agent host cannot read a paper back.
//  2. Each run of each item not handed in yet is an attempt of the
//     production executor with its pins: agentd's write wall over the
//     attempt's own directories, and checkPins. A host that cannot prove them
//     sits nothing (the platform takes no run without them) but a --dev
//     trial, which hands nothing in. The attempt's directories are a working
//     task's, <root>/tasks/<task>/a<n>: an empty workspace, or for an item
//     with a repository one commit holding the tree of the item's commit and
//     none of its history, refused when that tree has a chief-exam entry; a
//     new output, home and CLAUDE_CONFIG_DIR, so a new session; canaries of
//     random content in the home and beside the kept papers. A plan the
//     controller would not use (ParsePlan, as AcceptPlan reads it) gets its
//     second round as at work: a new session, the rework text appended
//     (store.ReworkText).
//  3. Where the seat read: every path its Read, Grep and Glob calls name (the
//     stream-json) must be in its workspace, and no canary's content in its
//     answer. A run that strays is sat once more, and handed in as out of its
//     workspace (not passed) when it strays again. This finds a read after
//     it: no wall keeps the seat from reading what agentd's user can (the
//     paper kept here among it; the exam's unit hides the rest), and an event
//     too long to read counts as a stray (review 415 B2, C1).
//  4. The answer, masked as a report is (handText) and so that the platform's
//     scans of it as kept take it (examText), is handed in at once, alone;
//     the run is recorded as handed in, and its directories go. Nothing of an
//     answer stays on the host.
//
// A run cut short (SIGTERM, or agentd killed: its record names the
// executor's process group, ended before the run is sat again), or whose
// executor left no answer of its own (a login or a vendor that failed), is
// not handed in and is sat again next time. A run out of its time is handed
// in failed and with no text, as the controller takes nothing of such an
// attempt; the platform passes no run that did not end done (review 415 A1,
// A2). A host that stops between the platform issuing a paper and keeping
// it here cannot go on with that paper: it waits for it to lapse (C2).

// examExecutors are the executors that sit an exam: claude, the chief of
// staff's seat.
var examExecutors = map[string]bool{"exec:claude": true}

// ExamExecutor refuses an executor that does not sit exams here.
func ExamExecutor(executor string) error {
	if !examExecutors[executor] {
		return fmt.Errorf("agentd: %s: only exec:claude sits an exam for now (the chief of staff's seat)", executor)
	}
	return nil
}

// Exam sits a paper of a job's exam with one executor on this host.
type Exam struct {
	Root        string // agentd's directory (AGENTD_ROOT): runs in tasks/, papers in exam/
	Self        string // agentd's executable: the launcher and the wall
	Client      *Client
	Remote      func(repo string) string
	GitEnv      []string
	Executor    Executor
	Tool        Tool
	ToolVersion string
	// Wall is what ProveWall found here: without one that holds the executor
	// no run proves its pins.
	Wall *Wall
	// Dev: a local trial, which hands nothing in and records nothing.
	Dev       bool
	Limit     time.Duration // one round of one run; 15 min
	KillGrace time.Duration // 10 s
	Retry     time.Duration // between hand-ins the platform could not take; 5 s
	Logf      func(format string, args ...any)

	// confine, in tests, stands for the wall and checkPins: the spawn as it
	// runs, and why its pins are not all there ("" when they are).
	confine func(a *Attempt, sp Spawn) (Spawn, string)
}

var (
	examJobRE  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	examItemRE = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)
	examSHARE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (e *Exam) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

// dir keeps the paper of job for the executor and the runs handed in.
func (e *Exam) dir(job string) string {
	return filepath.Join(e.Root, "exam", job+"-"+strings.TrimPrefix(e.Tool.Executor, "exec:"))
}

// unpinned says why no run here proves the executor's pins ("" when one
// can): only agentd's own wall, proven on this host, holds claude.
func (e *Exam) unpinned() string {
	if e.confine != nil || e.Wall.wraps(e.Tool.Executor) {
		return ""
	}
	return "no write wall holds " + e.Tool.Executor + " on this host (Landlock, proven at start)"
}

// ready refuses what cannot sit job's exam here: another executor, a job
// that is no name, a host without the executor's pins (but a trial).
func (e *Exam) ready(job string) error {
	if err := ExamExecutor(e.Tool.Executor); err != nil {
		return err
	}
	if !examJobRE.MatchString(job) {
		return fmt.Errorf("agentd: %q is not a job's name", job)
	}
	if why := e.unpinned(); why != "" && !e.Dev {
		return fmt.Errorf("agentd: %s: no run here proves its pins, and the platform takes no run without them; "+
			"--dev sits a local trial that hands nothing in", why)
	}
	return nil
}

// Paper is the paper this host sits: the one it keeps for job and the
// executor while the platform still takes its answers, or a new one, kept
// before anything runs. A kept paper that lapsed is let go: the platform
// grades it when a paper is next asked for, a run without an answer not
// passed.
func (e *Exam) Paper(ctx context.Context, job string) (*store.ExamPaper, error) {
	if err := e.ready(job); err != nil {
		return nil, err
	}
	dir := e.dir(job)
	p, err := readPaper(dir)
	switch {
	case err == nil && time.Now().Before(p.ExpiresAt):
		if cerr := checkPaper(p, job, e.Tool.Executor, e.Client.Host); cerr != nil {
			return nil, fmt.Errorf("%w: kept in %s, it is not sat here", cerr, dir)
		}
		handed, herr := readHanded(dir)
		if herr != nil {
			return nil, herr
		}
		e.logf("agentd exam: going on with paper %s (issued %s): %d of %d runs handed in; it takes answers until %s",
			p.RunID, p.IssuedAt.Format(time.RFC3339), len(handed), runsOf(p), p.ExpiresAt.Format(time.RFC3339))
		return p, nil
	case err == nil:
		handed, _ := readHanded(dir)
		e.logf("agentd exam: paper %s lapsed at %s with %d of %d runs handed in: the platform grades it when the next paper is asked "+
			"for, each run without an answer not passed", p.RunID, p.ExpiresAt.Format(time.RFC3339), len(handed), runsOf(p))
		// What a run of it cut short left goes first: the record of it goes
		// with the paper (review 415 B1).
		if err = e.clearSitting(dir); err != nil {
			return nil, err
		}
		if err = forceRemove(dir); err != nil {
			return nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	p, err = e.Client.ExamPaper(ctx, job, e.Tool.Executor)
	if errors.Is(err, ErrState) {
		return nil, fmt.Errorf("%w; this host keeps no paper to go on with (an Agent host cannot read one back): "+
			"the platform hands out no other until that one lapses", err)
	}
	if err != nil {
		return nil, err
	}
	if err = checkPaper(p, job, e.Tool.Executor, e.Client.Host); err != nil {
		return nil, fmt.Errorf("%w (it is open on the platform until %s)", err, p.ExpiresAt.Format(time.RFC3339))
	}
	if err = keepPaper(dir, p); err != nil {
		return nil, fmt.Errorf("agentd: keeping paper %s: %w (it is open on the platform until %s, and no run of it is sat without it)",
			p.RunID, err, p.ExpiresAt.Format(time.RFC3339))
	}
	e.logf("agentd exam: paper %s of %s for %s: %d items, %d runs; it takes answers until %s", p.RunID, job, e.Tool.Executor,
		len(p.Items), runsOf(p), p.ExpiresAt.Format(time.RFC3339))
	if e.Dev {
		e.logf("agentd exam: a trial hands nothing in; a run of this host without --dev goes on with paper %s", p.RunID)
	}
	return p, nil
}

func runsOf(p *store.ExamPaper) int {
	n := 0
	for _, it := range p.Items {
		n += len(it.Runs)
	}
	return n
}

func readPaper(dir string) (*store.ExamPaper, error) {
	b, err := os.ReadFile(filepath.Join(dir, "paper.json"))
	if err != nil {
		return nil, err
	}
	var p store.ExamPaper
	if err = json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("agentd: %s: %w", filepath.Join(dir, "paper.json"), err)
	}
	return &p, nil
}

func keepPaper(dir string, p *store.ExamPaper) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeDurably(filepath.Join(dir, "paper.json"), b)
}

// checkPaper refuses a paper that is not one of job's for the executor on
// host, or whose items an executor cannot sit as a working task.
func checkPaper(p *store.ExamPaper, job, executor, host string) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("agentd: paper %.12s: "+format, append([]any{p.RunID}, args...)...)
	}
	switch {
	case !examSHARE.MatchString(p.RunID) || !examSHARE.MatchString(p.SetSHA256):
		return bad("its run id and its set are sha256s")
	case p.Job != job || p.Executor != executor || p.Host != host:
		return bad("it is of %s for %s on %s, not of %s for %s on %s", p.Job, p.Executor, p.Host, job, executor, host)
	case !p.ExpiresAt.After(p.IssuedAt):
		return bad("it lapses before it was issued")
	case len(p.Items) == 0 || len(p.Items) > 100:
		return bad("a paper has 1 to 100 items")
	}
	seen := map[string]bool{}
	for _, it := range p.Items {
		if _, err := examSpec(it); err != nil {
			return bad("item %q: %v", it.Item, err)
		}
		if seen[it.Item] {
			return bad("item %s comes twice", it.Item)
		}
		seen[it.Item] = true
	}
	return nil
}

// examSpec is an item's spec as its executor reads it: text, as a brief's
// and a plan's are; no checks, setup, siblings or subject.
func examSpec(it store.ExamPaperItem) (TaskSpec, error) {
	runs := map[int]bool{}
	for _, n := range it.Runs {
		if n < 1 || runs[n] {
			return TaskSpec{}, errors.New("its runs are numbered from 1, each once")
		}
		runs[n] = true
	}
	switch {
	case !examItemRE.MatchString(it.Item):
		return TaskSpec{}, errors.New("an item is letters, digits, _ or -")
	case len(runs) == 0 || it.Rounds < 1 || it.Rounds > 2:
		return TaskSpec{}, errors.New("an item has runs, and 1 or 2 rounds")
	case (it.Repo == "") != (it.BaseCommit == ""):
		return TaskSpec{}, errors.New("an item with a repository has its commit, and only then")
	case it.Repo != "" && (!benchRepoRE.MatchString(it.Repo) || !benchSHARE.MatchString(it.BaseCommit)):
		return TaskSpec{}, errors.New("its repository is a name and its commit a full hex id")
	}
	s, err := ParseSpec(it.Spec)
	if err != nil {
		return s, err
	}
	if strings.TrimSpace(s.Text) == "" || len(s.Verify) > 0 || len(s.Setup) > 0 || len(s.Siblings) > 0 || s.Check != "" || s.SubjectSpec != "" {
		return s, errors.New("an exam item's spec is text alone: no checks, setup, siblings or subject")
	}
	return s, nil
}

// examKey is one run of one item.
type examKey struct {
	item string
	run  int
}

// examHanded is a run handed in, as this host keeps it: never its answer.
type examHanded struct {
	Item       string    `json:"item"`
	RunNo      int       `json:"run_no"`
	Round      int       `json:"round"`
	Conclusion string    `json:"conclusion"`
	OutOfWS    bool      `json:"out_of_workspace"`
	At         time.Time `json:"at"`
}

// readHanded reads the runs handed in. A last line cut short (the host
// stopped while it was written) is cut off the file: that run is sat again,
// and the platform keeps the answer it took first (review 415 B1).
func readHanded(dir string) (map[examKey]bool, error) {
	path := filepath.Join(dir, "handed.jsonl")
	out := map[examKey]bool{}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if end := bytes.LastIndexByte(b, '\n') + 1; end < len(b) {
		if err = os.Truncate(path, int64(end)); err != nil {
			return nil, err
		}
		b = b[:end]
	}
	for line := range bytes.Lines(b) {
		var r examHanded
		if err = json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("agentd: %s: %w", path, err)
		}
		out[examKey{r.Item, r.RunNo}] = true
	}
	return out, nil
}

// readJSONLines reads a file of one JSON value a line; none is no file.
func readJSONLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var v T
		if err = json.Unmarshal(sc.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("agentd: %s: %w", path, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}

// appendJSONLine adds v to a file of one JSON value a line.
func appendJSONLine(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Sit sits each run of the paper's items (only, when given) not handed in
// yet and hands it in as it ends. The platform grades the paper once every
// run has its answer; then nothing of it is kept here.
func (e *Exam) Sit(ctx context.Context, p *store.ExamPaper, only []string) error {
	if err := e.ready(p.Job); err != nil {
		return err
	}
	dir := e.dir(p.Job)
	items := p.Items
	if len(only) > 0 {
		names := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			names = append(names, it.Item)
		}
		for _, o := range only {
			if !slices.Contains(names, o) {
				return fmt.Errorf("agentd: paper %.12s has no item %q (it has %s)", p.RunID, o, strings.Join(names, ", "))
			}
		}
		items = slices.DeleteFunc(slices.Clone(p.Items), func(it store.ExamPaperItem) bool { return !slices.Contains(only, it.Item) })
	}
	handed, err := readHanded(dir)
	if err != nil {
		return err
	}
	if err = e.clearLeft(dir); err != nil {
		return err
	}
	total := runsOf(p)
	for _, it := range items {
		spec, serr := examSpec(it)
		if serr != nil {
			return serr
		}
		for _, n := range it.Runs {
			if handed[examKey{it.Item, n}] {
				continue
			}
			if err = ctx.Err(); err != nil {
				return e.stopped(p, len(handed), err)
			}
			s, rerr := e.sitRun(ctx, dir, it, spec, n)
			if rerr != nil {
				if ctx.Err() != nil {
					return e.stopped(p, len(handed), ctx.Err())
				}
				return fmt.Errorf("agentd: %s run %d: %w (nothing of it was handed in; it is sat again when paper %.12s is gone on with)",
					it.Item, n, rerr, p.RunID)
			}
			a := s.answer
			if e.Dev {
				e.logf("agentd exam: trial %s run %d: round %d, %s, %d s, %d bytes, model %s, pins: %s, out of its workspace: %v; not handed in",
					it.Item, n, a.Round, a.Conclusion, a.WallS, len(a.Text), s.model, orNone(s.pins), a.OutOfWorkspace)
				continue
			}
			out, herr := e.handIn(ctx, p, s)
			switch {
			case errors.Is(herr, ErrKeyConflict):
				// Handed in before, by a run of this host whose record did not
				// last: the platform keeps the first.
				e.logf("agentd exam: %s run %d was handed in before: %v", it.Item, n, herr)
			case herr != nil:
				return fmt.Errorf("agentd: handing in %s run %d: %w (it is sat again when paper %.12s is gone on with)", it.Item, n, herr, p.RunID)
			default:
				e.logf("agentd exam: %s run %d handed in: round %d, %s, %d s, out of its workspace: %v; %v of %v runs answered",
					it.Item, n, a.Round, a.Conclusion, a.WallS, a.OutOfWorkspace, out["answered"], out["expected"])
			}
			if err = appendJSONLine(filepath.Join(dir, "handed.jsonl"), examHanded{Item: it.Item, RunNo: n, Round: a.Round,
				Conclusion: a.Conclusion, OutOfWS: a.OutOfWorkspace, At: time.Now().UTC()}); err != nil {
				return err
			}
			handed[examKey{it.Item, n}] = true
			if graded, _ := out["graded"].(bool); graded {
				e.logf("agentd exam: paper %s is graded (event %v, %v rows); nothing of it is kept here", p.RunID, out["event_id"], out["rows"])
				return forceRemove(dir)
			}
		}
	}
	switch {
	case e.Dev:
		e.logf("agentd exam: trial of paper %s done; nothing was handed in", p.RunID)
	case len(handed) >= total:
		// The last answer was taken before, and graded the paper then.
		e.logf("agentd exam: every run of paper %s is handed in, so it is graded; nothing of it is kept here", p.RunID)
		return forceRemove(dir)
	default:
		e.logf("agentd exam: paper %s: %d of %d runs handed in; run agentd exam again to go on with it until %s", p.RunID, len(handed), total,
			p.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "all there"
	}
	return s
}

// stopped is a sitting stopped from outside: the run it was in is sat again.
func (e *Exam) stopped(p *store.ExamPaper, handed int, err error) error {
	return fmt.Errorf("agentd: stopped (%w): %d of %d runs of paper %.12s handed in; run agentd exam again to go on with it until %s",
		err, handed, runsOf(p), p.RunID, p.ExpiresAt.Format(time.RFC3339))
}

// handIn hands in one run, as one part. A run that ended is handed in even
// while agentd stops: it would otherwise be sat again.
func (e *Exam) handIn(ctx context.Context, p *store.ExamPaper, s sat) (map[string]any, error) {
	sub := store.ExamSubmission{RunID: p.RunID, Executor: p.Executor, Provider: e.Tool.Provider, Model: s.model, ToolVersion: e.ToolVersion,
		ExamSHA256: p.SetSHA256, PinsOK: s.pins == "", Answers: []store.ExamAnswer{s.answer}}
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	var out map[string]any
	err := retryUnavailable(hctx, orDefault(e.Retry, 5*time.Second), func() error {
		var perr error
		out, perr = e.Client.ExamAnswers(hctx, sub)
		return perr
	})
	return out, err
}

// clearLeft ends what a run cut short left (clearSitting), and, but in a
// trial, refuses to sit beside any other work under <root>/tasks: a seat
// could read another task's checkout there (the exam's unit hides it all).
func (e *Exam) clearLeft(dir string) error {
	if err := e.clearSitting(dir); err != nil {
		return err
	}
	if e.Dev {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(e.Root, "tasks"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("agentd: %s holds %d entries of other work (%s, …): a seat could read them; an exam is sat with none there",
			filepath.Join(e.Root, "tasks"), len(entries), entries[0].Name())
	}
	return nil
}

// clearSitting ends what a run cut short left, as its record in dir has it:
// its executor's process group while that is still the run's (ClearGroup),
// then its task's directories, then the record. A group that cannot be
// shown gone keeps all three, and nothing is sat (review 415 A2).
func (e *Exam) clearSitting(dir string) error {
	path := filepath.Join(dir, "sitting")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var s examSitting
	if json.Unmarshal(b, &s) != nil || !taskName.MatchString(s.Task) || s.PGID < 0 || (s.PGID > 0) != (s.Start != "") {
		return fmt.Errorf("agentd: %s is not a run's record: what that run left cannot be found; look and remove it by hand", path)
	}
	if s.PGID > 0 {
		if state, pids := ClearGroup(s.PGID, s.Start, time.Time{}, orDefault(e.KillGrace, 10*time.Second)); state != GroupCleared {
			return fmt.Errorf("agentd: the run cut short in task %s left its process group %d (%s; processes %v): end them by hand; "+
				"nothing is sat until they are gone", s.Task, s.PGID, state, pids)
		}
	}
	if err = forceRemove(filepath.Join(e.Root, "tasks", s.Task)); err != nil {
		return err
	}
	return os.Remove(path)
}
