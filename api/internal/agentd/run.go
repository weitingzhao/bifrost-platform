package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Running one attempt (design v5 §5.2 「agentd」): fetch the spec, prepare the
// workspace, launch the executor through the launcher, keep the lease while
// it runs, and hand the result over: computed, artifacts, the attempt's
// branch, persist, submit, ended. A lease the platform says is not ours, or
// that cannot be renewed three times running, ends the process group, and
// what it left goes to the platform only by salvage (§6.2).

// Attempt is what an executor is given.
type Attempt struct {
	Claim   *Claimed
	Spec    TaskSpec
	Subject *TaskSpec // a check's subject spec
	// VerifyReport is the verify.json of the deterministic check a review
	// follows.
	VerifyReport []byte
	// Failed are the checks the task's earlier rounds failed, with their
	// reports, for an implementation (TD-405, lastround.go); SubjectReport
	// is what the attempt a review checks handed in (TD-409, subject.go).
	Failed        []PriorCheck
	SubjectReport []byte
	Dir           string // the workspace
	Out           string // where the executor writes report.md or verify.json
	// Walled: the executor runs inside agentd's write wall (confine.go);
	// inner is what the code it runs besides itself may write (innerWrites),
	// walled whenever the host has a wall.
	Walled bool
	inner  []string
	root   string       // agentd's directory, which Dir and Out are in (openWithin)
	testDB *TestDBTools // its test database's programs, when it has one (testdb.go)
}

// An Executor turns an attempt into the command agentd launches.
type Executor func(a *Attempt) (Spawn, error)

// Daemon is one host's agentd.
type Daemon struct {
	Client    *Client
	Registry  *Registry
	Root      string // tasks/<task>/a<n> live here
	Self      string // agentd's executable: the launcher
	Executors map[string]Executor
	Remote    func(repo string) string // a repository's git remote, where work is cloned from
	// PushRemote is where attempt branches go, and where a check finds the
	// commit it checks; nil is Remote (a repository that takes pushes).
	PushRemote func(repo string) string
	GitEnv     []string
	Logf       func(format string, args ...any)

	Beat            time.Duration // heartbeat; 30 s
	MaxFails        int           // heartbeats failing in a row before the lease counts as lost; 3
	KillGrace       time.Duration // TERM to KILL; 10 s
	SalvageEvery    time.Duration // salvage retry while in grace; 60 s
	CheckpointEvery time.Duration // log tail and patch while the executor runs; 5 min
	SampleLimit     time.Duration // how long one checkpoint may take; 2 min
	TakeOverLimit   time.Duration // how long one takeover step may take; 5 min
	Retry           time.Duration // pause between retries of a step the platform could not take; 5 s

	next map[string]time.Time // when a pending hand-over is tried again

	// live are the attempts this process runs or hands over now. Several
	// run at once (W-31 2.4 slice ③); Recover leaves these to their Run and
	// takes over only what a previous process left.
	mu    sync.Mutex
	live  map[string]bool
	share hostShare
	// codexTurn: one codex attempt at a time, each with a copy of the host's
	// login, which a refresh in one would spend for the others (review 300
	// B5).
	codexTurn hostShare
	// Wall is what ProveWall found on this host: the executors that run
	// inside the write wall, and so may share it (share.go). nil walls none.
	Wall *Wall
	// CodexHome is the host user's codex home, whose login each codex
	// attempt gets a copy of and gives back refreshed (codexlogin.go).
	CodexHome      string
	KeepWorkspaces bool // settled attempts' workspaces stay as they were (removeWorkspace, TD-415)
	// TestDB is the programs a test database needs on this host (TD-403,
	// testdb.go); nil: no attempt gets one, and its log says so. testDBSwept
	// is when Recover last swept what test databases left.
	TestDB      *TestDBTools
	testDBSwept time.Time
	// Confined, in tests, says which attempts may share the host instead of
	// the wall (share.go).
	Confined func(cl *Claimed, prep TaskSpec) bool
	// afterLoad, in tests, runs between Recover's read of the registry and
	// its choice of what to take over.
	afterLoad func()
}

// gitWall is the wall agentd's own git runs in, in a checkout an executor
// had (Workspace.wall): any host with a write wall has it.
func (d *Daemon) gitWall(dir string) []string {
	if d.Wall == nil {
		return nil
	}
	return wallArgv(d.Self, []string{dir})
}

// takeHost holds the host for an attempt, alone or shared, and for a codex
// attempt codex's turn first (codexTurn): always in that order, so no two
// attempts wait for each other.
func (d *Daemon) takeHost(ctx context.Context, cl *Claimed, alone bool) (func(), error) {
	turn := func() {}
	if cl.Tool.Executor == "exec:codex" {
		r, err := d.codexTurn.take(ctx, true)
		if err != nil {
			return nil, err
		}
		turn = r
	}
	release, err := d.share.take(ctx, alone)
	if err != nil {
		turn()
		return nil, err
	}
	return func() { release(); turn() }, nil
}

// failBefore ends an attempt that failed after it was registered and before
// its executor was released, as exit with the error (review 278 B); an
// agentd that is shutting down leaves the entry for Recover instead.
// A stop or a lost lease that came while it was prepared takes its own path
// first, as it would at the launch (review 279 B).
func (d *Daemon) failBefore(ctx context.Context, a *Attempt, ws *Workspace, e *Entry, l *lease, err error) (Outcome, error) {
	removeCursorData(d.Root, a.Out, d.logf) // made by cursor's builder, never used (review 360 B1)
	switch {
	case ctx.Err() != nil:
		return Outcome{}, err
	case l.isLost():
		out, lerr := d.lost(ctx, a, ws, e)
		return out, errors.Join(err, lerr)
	case l.isStopped():
		out, serr := d.unlaunched(ctx, a, ws, e, l)
		return out, errors.Join(err, serr)
	}
	d.logf("agentd: %s ends before its executor started: %v", e.AttemptID, err)
	whole, rerr := d.reportPrepare(ctx, a, e, beforeReport(err)) // why goes first (TD-394); taken whole, it is all the attempt left, unless the workspace is an earlier run's (R2; review 499 A-1)
	if e.HandedOver = whole && !errors.Is(err, ErrWorkspaceExists); rerr != nil {
		d.logf("agentd: %s: what failed before its executor started was not handed in: %v", e.AttemptID, rerr)
	}
	return Outcome{EndHow: prepareEnd, Conclusion: "none"}, errors.Join(err, d.end(ctx, e, prepareEnd))
}

// blockedBy says which attempt's process group holds the host, or nothing.
func blockedBy(id string) error {
	if id == "" {
		return nil
	}
	return fmt.Errorf("agentd: the process group of %s is not gone; nothing starts beside it", id)
}

// MaxSlots is how many attempts agentd asks for at most; the platform leases
// no more than the signed concurrency rule's per_host.
const MaxSlots = 3

// Hold marks an attempt as run by this process, before its Run starts.
func (d *Daemon) Hold(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.live == nil {
		d.live = map[string]bool{}
	}
	d.live[id] = true
}

func (d *Daemon) drop(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.live, id)
}

func (d *Daemon) isLive(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.live[id]
}

// Running is how many attempts this process runs now.
func (d *Daemon) Running() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.live)
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

func (d *Daemon) pushRemote(repo string) string {
	if d.PushRemote != nil {
		return d.PushRemote(repo)
	}
	return d.Remote(repo)
}

// taskName is what a task id may be in a refspec agentd builds.
var taskName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// checkout prepares the attempt's workspace at its base. A check's base is
// the commit the checked attempt pushed: when the clone does not have it, the
// checked task's branches are fetched from where branches go, and nothing
// else.
func (d *Daemon) checkout(ctx context.Context, a *Attempt, repo, base string) (*Workspace, error) {
	if a.Subject == nil || d.PushRemote == nil {
		return prepareIn(ctx, d.Root, a.Dir, d.Remote(repo), base, d.GitEnv, Fetch{})
	}
	task := a.Spec.SubjectTask
	if !taskName.MatchString(task) {
		return nil, fmt.Errorf("agentd: the checked task %q is not a task id", task)
	}
	return prepareIn(ctx, d.Root, a.Dir, d.Remote(repo), base, d.GitEnv, Fetch{Remote: d.PushRemote(repo),
		Refspec: "+refs/heads/agent/" + task + "/*:refs/remotes/agents/" + task + "/*"})
}

func orDefault(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// Outcome is how an attempt ended here.
type Outcome struct {
	EndHow     string
	ExitCode   int
	Conclusion string
	Submitted  bool
	Salvaged   bool
}

// Run runs one claimed attempt to its end. It holds the attempt for as long
// as it runs, so that Recover, which runs between claims, leaves it alone.
func (d *Daemon) Run(ctx context.Context, cl *Claimed) (Outcome, error) {
	d.Hold(cl.AttemptID)
	defer d.drop(cl.AttemptID)
	a := &Attempt{Claim: cl}
	l := d.keepLease(ctx, cl.AttemptID, cl.Epoch) // from the claim to the hand-over (review 163 A7)
	defer l.release()
	body, err := d.Client.GetArtifact(ctx, cl.SpecArtifact)
	if err != nil {
		return Outcome{}, err
	}
	if a.Spec, err = ParseSpec(body); err != nil {
		return Outcome{}, err
	}
	repo, base := cl.Repo, cl.BaseCommit
	if a.Spec.Check != "" {
		var sb []byte
		if sb, err = d.Client.GetArtifact(ctx, a.Spec.SubjectSpec); err != nil {
			return Outcome{}, err
		}
		var subject TaskSpec
		if subject, err = ParseSpec(sb); err != nil {
			return Outcome{}, err
		}
		a.Subject = &subject
		repo, base = a.Spec.Repo, a.Spec.Commit
		if a.Spec.VerifyReport != "" {
			if a.VerifyReport, err = d.Client.GetArtifact(ctx, a.Spec.VerifyReport); err != nil {
				return Outcome{}, err
			}
		}
	}
	if a.Failed, err = d.priorChecks(ctx, a); err == nil {
		a.SubjectReport, err = d.subjectReport(ctx, a)
	}
	if err != nil {
		return Outcome{}, err
	}
	a.Dir = WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)
	a.Out, a.root = a.Dir+".out", d.Root
	// The host is held from here, before anything is written, to the end
	// (review 276 A2); a process group still standing from an earlier attempt
	// may write anywhere, so nothing starts beside one. While it waits for the
	// host the attempt is registered as launching, and a stop or a lost lease
	// ends the wait as it would end a launch (review 277 A3): a restart, or a
	// standing group, leaves the entry for Recover to end.
	waiting := Entry{AttemptID: cl.AttemptID, TaskID: cl.TaskID, AttemptNo: cl.AttemptNo, Epoch: cl.Epoch, Role: cl.Role, Repo: cl.Repo,
		Effect: cl.EffectClass, PushedRef: cl.PushedRef, WorkID: cl.WorkID, Workdir: a.Dir, Base: base, Phase: "launching",
		Executor: cl.Tool.Executor, Group: GroupCleared}
	if err = d.Registry.Put(waiting); err != nil {
		return Outcome{}, err
	}
	wctx, wcancel := context.WithCancel(ctx)
	watched := make(chan struct{})
	safego.Go("agentd-share-watch", func() {
		select {
		case <-l.lost:
		case <-l.stop:
		case <-watched:
		}
		wcancel()
	})
	// Held to its own directories: inside agentd's wall, or codex in its own
	// sandbox proven on this host (confine.go).
	confined := d.Wall.holds(cl.Tool.Executor)
	a.Walled = d.Wall.wraps(cl.Tool.Executor)
	isConfined := func(*Claimed, TaskSpec) bool { return confined }
	if d.Confined != nil {
		isConfined = d.Confined
	}
	release, err := d.takeHost(wctx, cl, !isConfined(cl, a.prepare()))
	close(watched)
	wcancel()
	switch {
	case err == nil:
	case l.isLost():
		return d.lost(ctx, a, nil, &waiting)
	case l.isStopped():
		return d.unlaunched(ctx, a, nil, &waiting, l)
	default:
		return Outcome{}, err
	}
	defer release()
	if id, berr := d.Blocked(); berr != nil || id != "" {
		return d.failBefore(ctx, a, nil, &waiting, l, errors.Join(berr, blockedBy(id)))
	}
	var ws *Workspace
	switch {
	case repo != "" && base != "":
		if ws, err = d.checkout(ctx, a, repo, base); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
	default:
		if _, err = os.Stat(a.Dir); err == nil {
			return d.failBefore(ctx, a, ws, &waiting, l, ErrWorkspaceExists)
		}
		if err = os.MkdirAll(a.Dir, 0o700); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
	}
	if ws != nil {
		ws.wall, ws.root = d.gitWall(a.Dir), d.Root // from here the checkout is the executor's
	}
	// The output is new, as the workspace is: the attempt's home is in it,
	// and nothing an earlier run left there may be read (TD-371).
	if err = os.Mkdir(a.Out, 0o700); err != nil {
		return d.failBefore(ctx, a, ws, &waiting, l, fmt.Errorf("agentd: the attempt's output: %w", err))
	}
	// Siblings and setup belong with a checkout: a task with none has nothing
	// for them to sit beside or install into (setup.go).
	prep := a.prepare()
	var sibs []string
	if ws != nil {
		if err = d.siblings(ctx, a, prep.Siblings); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
		sibs = siblingDirs(a, prep.Siblings)
	}
	a.inner = innerWrites(a, sibs)
	exec, ok := d.Executors[cl.Tool.Executor]
	if !ok {
		return d.failBefore(ctx, a, ws, &waiting, l, fmt.Errorf("agentd: no executor %s on this host", cl.Tool.Executor))
	}
	sp, err := exec(a)
	if err != nil {
		return d.failBefore(ctx, a, ws, &waiting, l, err)
	}
	noDB := false
	if ws != nil {
		if sp, noDB, err = d.testDBFor(a, repo, cl.Tool.Executor, sp); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
	}
	if ws != nil && len(prep.Setup) > 0 {
		// The setup has a home of its own and, on a host with a wall, a
		// narrower wall, the executor in it or not (codex): it cannot leave
		// configuration for the executor; what may not be in the checkout is
		// looked for again once it is done (review 300 A3).
		o := setupOpts{Home: setupHome(a.Out)}
		if d.Wall != nil {
			o.Writes = a.inner
		}
		if cl.Tool.Executor == "exec:cursor" {
			o.Refuse = cursorRefused
		}
		if sp, err = withSetup(d.Self, a.Out, prep.Setup, sp, o); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
	}
	// The wall goes around the setup and the executor alike; a confined
	// attempt runs beside others, so it does not run without every pin.
	if a.Walled {
		sp.Argv = append(wallArgv(d.Self, executorWrites(cl.Tool.Executor, a, sibs)), sp.Argv...)
	}
	if confined {
		if err = checkPins(d.Wall, cl.Tool.Executor, d.Self, a, sibs, sp); err != nil {
			return d.failBefore(ctx, a, ws, &waiting, l, err)
		}
	}
	logf, err := os.OpenFile(filepath.Join(a.Out, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return d.failBefore(ctx, a, ws, &waiting, l, err)
	}
	defer func() { _ = logf.Close() }()
	if noDB {
		_, _ = fmt.Fprintln(logf, noTestDB)
	}
	sp.Stdout, sp.Stderr = logf, logf
	if t := newActionTail(cl.Tool.Executor, filepath.Join(a.Out, "log.txt"), a.Dir, a.Out); t != nil {
		l.tail.Store(t)
		tctx, tcancel := context.WithCancel(ctx)
		defer tcancel()
		safego.Go("agentd-actions", func() { t.collect(tctx) })
	}
	entry := Entry{AttemptID: cl.AttemptID, TaskID: cl.TaskID, AttemptNo: cl.AttemptNo, Epoch: cl.Epoch, Role: cl.Role, Repo: cl.Repo,
		Effect: cl.EffectClass, PushedRef: cl.PushedRef, WorkID: cl.WorkID, Workdir: a.Dir, Base: base, Phase: "launching",
		Executor: cl.Tool.Executor}
	// A lost lease or a stop that came while the workspace was prepared, or
	// while the start was reported, is acted on before the executor is
	// released (review 164 A7).
	if l.isLost() {
		return d.lost(ctx, a, ws, &entry)
	}
	if l.isStopped() {
		return d.unlaunched(ctx, a, ws, &entry, l)
	}
	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	safego.Go("agentd-start-watch", func() {
		select {
		case <-l.lost:
		case <-l.stop:
		case <-sctx.Done():
		}
		scancel()
	})
	// The group is recorded and the start reported before the executor is
	// released: an executor never runs unrecorded, nor before the platform
	// knows it started (review 163 A7).
	proc, err := Launch(d.Self, sp, func(pgid int, start string) error {
		entry.PGID, entry.Start, entry.Phase = pgid, start, "running"
		if perr := d.Registry.Put(entry); perr != nil {
			return perr
		}
		if serr := d.retry(sctx, func() error { return d.Client.Start(sctx, cl.AttemptID, cl.Epoch, cl.Tool.Model, cl.Tool.Provider) }); serr != nil {
			return serr
		}
		if l.isLost() || l.isStopped() {
			return errors.New("agentd: the lease was lost or the attempt stopped before its release")
		}
		return nil
	})
	if err != nil {
		if entry.PGID == 0 {
			removeCursorData(d.Root, a.Out, d.logf) // nothing started (review 360 B1)
			return Outcome{}, err
		}
		switch {
		case errors.Is(err, ErrFenced) || l.isLost():
			return d.lost(ctx, a, ws, &entry)
		case l.isStopped():
			return d.unlaunched(ctx, a, ws, &entry, l)
		}
		return Outcome{EndHow: "exit", Conclusion: "none"}, errors.Join(err, d.end(ctx, &entry, "exit"))
	}

	endHow, code := d.supervise(ctx, a, proc, ws, &entry, l)
	if entry.Group == GroupCleared {
		afterExecutor(cl.Tool.Executor, d.CodexHome, d.Root, a.Out, d.logf)
	}
	if endHow != "lease_lost" {
		d.lastActions(ctx, cl.AttemptID, cl.Epoch, l)
	}
	if sp.After != nil {
		if err := sp.After(); err != nil {
			d.logf("agentd: after %s: %v", cl.AttemptID, err)
		}
	}
	if endHow == "lease_lost" {
		return d.lost(ctx, a, ws, &entry)
	}
	exit := code
	entry.Phase, entry.ExitCode, entry.EndHow = "exited", &exit, endHow
	entry.Conclusion = a.conclusion(endHow, code, a.results())
	if entry.SetupFailed = isSetupFailure(endHow, code, a.Out); entry.SetupFailed {
		entry.EndHow = prepareEnd
	}
	if err := d.Registry.Put(entry); err != nil {
		return Outcome{}, err
	}
	return d.deliver(ctx, a, ws, &entry, l)
}

// unlaunched hands over an attempt stopped before its executor was released:
// cancelled, concluding nothing.
func (d *Daemon) unlaunched(ctx context.Context, a *Attempt, ws *Workspace, entry *Entry, l *lease) (Outcome, error) {
	entry.Phase, entry.EndHow, entry.Conclusion = "exited", "cancelled", "none"
	if err := d.Registry.Put(*entry); err != nil {
		return Outcome{}, err
	}
	return d.deliver(ctx, a, ws, entry, l)
}

// supervise watches the executor under its lease and returns how it ended:
// exit, hard_limit (the task's limit from the release), cancelled (the
// platform asked it to stop), lease_lost, or daemon_restart (agentd shuts
// down). Whatever ends it ends its whole group, and supervise returns only
// once the end is done and the group checked (entry.Group). Checkpoints run
// beside it, one at a time and each within SampleLimit, so a slow upload
// never holds up a stop, a lost lease or the hard limit (review 164 A7).
func (d *Daemon) supervise(ctx context.Context, a *Attempt, proc *Proc, ws *Workspace, entry *Entry, l *lease) (string, int) {
	cl := a.Claim
	exited := make(chan int, 1)
	safego.Go("agentd-attempt-wait", func() { exited <- proc.Wait() })
	var hard <-chan time.Time
	if cl.HardLimitS > 0 {
		t := time.NewTimer(time.Duration(cl.HardLimitS) * time.Second)
		defer t.Stop()
		hard = t.C
	}
	sample := time.NewTicker(orDefault(d.CheckpointEvery, 5*time.Minute))
	defer sample.Stop()
	grace := orDefault(d.KillGrace, 10*time.Second)
	done, lost, stopped := ctx.Done(), l.lost, l.stop
	end := ""
	var terminated chan struct{}
	stop := func(why string) {
		done, lost, stopped, hard = nil, nil, nil, nil // each fires once
		if end == "" {
			end = why
			t := make(chan struct{})
			terminated = t
			safego.Go("agentd-terminate", func() { defer close(t); Terminate(proc.PGID, grace) })
		}
	}
	var sampling chan struct{}
	cancelSample := func() {}
	for {
		select {
		case code := <-exited:
			// The group is cleared first, on a copy, while a sample may still
			// be finishing: a sample that is slow to stop never holds up the
			// end of what the attempt left running (review 165 A2). The
			// group's state goes into the entry once the sample is done.
			died := time.Now()
			cancelSample()
			if terminated != nil {
				<-terminated
			}
			cleared := Entry{AttemptID: entry.AttemptID, PGID: entry.PGID, Start: entry.Start} // fields no sample writes
			d.clear(&cleared, died)
			if sampling != nil {
				<-sampling
			}
			entry.Group = cleared.Group
			if end == "" {
				end = "exit"
			}
			return end, code
		case <-hard:
			stop("hard_limit")
		case <-done:
			stop("daemon_restart")
		case <-lost:
			stop("lease_lost")
		case <-stopped:
			stop("cancelled")
		case <-sample.C:
			if end != "" || (sampling != nil && !closed(sampling)) {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, orDefault(d.SampleLimit, 2*time.Minute))
			s := make(chan struct{})
			sampling, cancelSample = s, cancel
			safego.Go("agentd-checkpoint", func() {
				defer close(s)
				defer cancel()
				d.checkpoint(sctx, a, ws, entry)
			})
		}
	}
}

// results reads what the executor left: the report (verify.json for a
// verification) and the tail of its log. Neither file is read further than
// what is kept (review 164 B1).
type results struct {
	report, reportKind string
	reportBody         []byte
	logTail            []byte
}

const logTailBytes = 64 << 10

func (a *Attempt) results() results {
	var r results
	switch a.Claim.Role {
	case "verify":
		r.reportKind, r.report = "verify", "verify.json"
	case "review":
		r.reportKind, r.report = "review", "report.md"
	default:
		r.reportKind, r.report = "report", "report.md"
	}
	if b, err := readAtMost(filepath.Join(a.Out, r.report), maxBody); err == nil && len(b) > 0 {
		r.reportBody = b
	}
	r.logTail, _ = readTail(filepath.Join(a.Out, "log.txt"), logTailBytes)
	return r
}

// errTooLarge: a file over what is read of it.
var errTooLarge = errors.New("agentd: the file is over its limit")

// readAtMost reads a file of at most n bytes; a longer one is not read (one
// that grows while it is read, not past n+1). Like readTail it opens only a
// regular file, never through a link, and never waits on a FIFO the executor
// left in its place (review 303 B).
func readAtMost(path string, n int) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("agentd: %s is not a regular file", path)
	}
	if info.Size() > int64(n) {
		return nil, errTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(n)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > n {
		return nil, errTooLarge
	}
	return b, nil
}

// readTail reads the last n bytes of a file, and nothing before them.
func readTail(path string, n int64) ([]byte, error) {
	f, err := openRegular(path, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// conclusion is the attempt's own verdict: a verification by its verify.json,
// any other by a clean exit with a report; an attempt that did not end on
// its own concludes nothing.
func (a *Attempt) conclusion(endHow string, code int, r results) string {
	switch {
	case endHow == "cancelled" || endHow == "daemon_restart":
		return "none"
	case endHow != "exit" || code != 0 || r.reportBody == nil:
		return "failed"
	case a.Claim.Role == "verify":
		var v Verification
		if json.Unmarshal(r.reportBody, &v) != nil || !v.Pass {
			return "failed"
		}
	case a.Claim.Role == "review":
		return reviewVerdict(r.reportBody)
	}
	return "done"
}

func (d *Daemon) putRetried(ctx context.Context, kind, media string, body []byte) (string, error) {
	var id string
	err := d.retry(ctx, func() error {
		var err error
		id, err = d.Client.PutArtifact(ctx, kind, media, body)
		return err
	})
	return id, err
}

// retry runs a platform step until it is taken or refused: only an
// unavailable platform is retried, a few times.
func (d *Daemon) retry(ctx context.Context, step func() error) error {
	return retryUnavailable(ctx, orDefault(d.Retry, 5*time.Second), step)
}

// retryUnavailable runs step up to five times, pause apart, while the
// platform is unavailable.
func retryUnavailable(ctx context.Context, pause time.Duration, step func() error) error {
	var err error
	for i := 0; i < 5; i++ {
		if err = step(); err == nil || !errors.Is(err, ErrUnavailable) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	return err
}

// The verdict line a review's report ends with (review 163 A2): the platform
// takes a review's done as a pass, so a review passes only when its report
// says so, on its last line; FAIL is failed, and a report without the line
// concludes nothing.
const (
	VerdictPass = "VERDICT: PASS"
	VerdictFail = "VERDICT: FAIL"
)

func reviewVerdict(report []byte) string {
	lines := strings.Split(strings.TrimSpace(string(report)), "\n")
	switch strings.TrimSpace(lines[len(lines)-1]) {
	case VerdictPass:
		return "done"
	case VerdictFail:
		return "failed"
	}
	return "none"
}
