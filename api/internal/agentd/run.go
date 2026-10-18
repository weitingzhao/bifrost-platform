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
	Dir          string // the workspace
	Out          string // where the executor writes report.md or verify.json
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
		return Prepare(ctx, a.Dir, d.Remote(repo), base, d.GitEnv)
	}
	task := a.Spec.SubjectTask
	if !taskName.MatchString(task) {
		return nil, fmt.Errorf("agentd: the checked task %q is not a task id", task)
	}
	return PrepareFetching(ctx, a.Dir, d.Remote(repo), base, d.GitEnv, Fetch{Remote: d.PushRemote(repo),
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

// Run runs one claimed attempt to its end.
func (d *Daemon) Run(ctx context.Context, cl *Claimed) (Outcome, error) {
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
	a.Dir = WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)
	a.Out = a.Dir + ".out"
	var ws *Workspace
	switch {
	case repo != "" && base != "":
		if ws, err = d.checkout(ctx, a, repo, base); err != nil {
			return Outcome{}, err
		}
	default:
		if _, err = os.Stat(a.Dir); err == nil {
			return Outcome{}, ErrWorkspaceExists
		}
		if err = os.MkdirAll(a.Dir, 0o700); err != nil {
			return Outcome{}, err
		}
	}
	if err = os.MkdirAll(a.Out, 0o700); err != nil {
		return Outcome{}, err
	}
	// Siblings and setup belong with a checkout: a task with none has nothing
	// for them to sit beside or install into (setup.go).
	prep := a.prepare()
	if ws != nil {
		if err = d.siblings(ctx, a, prep.Siblings); err != nil {
			return Outcome{}, err
		}
	}
	exec, ok := d.Executors[cl.Tool.Executor]
	if !ok {
		return Outcome{}, fmt.Errorf("agentd: no executor %s on this host", cl.Tool.Executor)
	}
	sp, err := exec(a)
	if err != nil {
		return Outcome{}, err
	}
	if ws != nil && len(prep.Setup) > 0 {
		if sp, err = withSetup(d.Self, a.Out, prep.Setup, sp); err != nil {
			return Outcome{}, err
		}
	}
	logf, err := os.OpenFile(filepath.Join(a.Out, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = logf.Close() }()
	sp.Stdout, sp.Stderr = logf, logf
	if t := newActionTail(cl.Tool.Executor, filepath.Join(a.Out, "log.txt"), a.Dir); t != nil {
		l.tail.Store(t)
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
// that grows while it is read, not past n+1).
func readAtMost(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
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
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if off := info.Size() - n; off > 0 {
		if _, err = f.Seek(off, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(f, n))
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
	var err error
	for i := 0; i < 5; i++ {
		if err = step(); err == nil || !errors.Is(err, ErrUnavailable) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(orDefault(d.Retry, 5*time.Second)):
		}
	}
	return err
}

// patchLimit: a checkpoint's patch over 1 MiB is recorded as too large, not
// kept (design v5 §5.2 检查点).
const patchLimit = 1 << 20

// checkpoint samples a running attempt (design v5 §5.2 检查点): the log's
// tail and the workspace's patch against the base, which is its net change,
// the executor's own commits included. A sample's request (its number, the
// artifacts it names, whether the patch was too large) is written to the
// registry before it is sent and resent as it is, until the platform
// confirms it; only then is the next sample taken (review 164 A6). A sample
// that cannot be taken now is left for the next tick.
func (d *Daemon) checkpoint(ctx context.Context, a *Attempt, ws *Workspace, entry *Entry) {
	if entry.Pending == nil {
		p := &PendingCheckpoint{Seq: entry.CheckpointSeq + 1}
		if tail, _ := readTail(filepath.Join(a.Out, "log.txt"), logTailBytes); len(tail) > 0 {
			id, err := d.Client.PutArtifact(ctx, "log_tail", "text/plain; charset=utf-8", tail)
			switch {
			case err == nil:
				p.Artifacts = append(p.Artifacts, id)
			case transient(err):
				d.logf("agentd: checkpoint %s: log tail: %v", entry.AttemptID, err)
				return
			}
		}
		if ws != nil {
			patch, tooLarge, err := ws.Diff(ctx, patchLimit)
			switch {
			case err != nil:
				d.logf("agentd: checkpoint %s: patch: %v", entry.AttemptID, err)
				return
			case tooLarge:
				p.TooLarge = true
			case len(patch) > 0:
				id, err := d.Client.PutArtifact(ctx, "checkpoint_patch", "text/x-diff", patch)
				switch {
				case err == nil:
					p.Artifacts = append(p.Artifacts, id)
				case transient(err):
					d.logf("agentd: checkpoint %s: patch: %v", entry.AttemptID, err)
					return
				}
			}
		}
		entry.Pending, entry.CheckpointSeq = p, p.Seq
	}
	// The request is on disk before it is sent, every time: a pending one
	// whose first write failed is written again first (review 165 A1).
	if err := d.Registry.Put(*entry); err != nil {
		d.logf("agentd: checkpoint %s: %v", entry.AttemptID, err)
		return
	}
	p := entry.Pending
	if err := d.Client.Checkpoint(ctx, entry.AttemptID, entry.Epoch, p.Seq, p.Artifacts, p.TooLarge); err != nil {
		d.logf("agentd: checkpoint %s #%d: %v", entry.AttemptID, p.Seq, err)
		return
	}
	entry.Confirmed, entry.Pending = p.Seq, nil
	if err := d.Registry.Put(*entry); err != nil {
		d.logf("agentd: checkpoint %s: %v", entry.AttemptID, err)
	}
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
