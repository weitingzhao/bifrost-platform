package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Remote    func(repo string) string // a repository's git remote
	GitEnv    []string
	Logf      func(format string, args ...any)

	Beat            time.Duration // heartbeat; 30 s
	MaxFails        int           // heartbeats failing in a row before the lease counts as lost; 3
	KillGrace       time.Duration // TERM to KILL; 10 s
	SalvageEvery    time.Duration // salvage retry while in grace; 60 s
	CheckpointEvery time.Duration // log tail and patch while the executor runs; 5 min
	Retry           time.Duration // pause between retries of a step the platform could not take; 5 s
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
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
		if ws, err = Prepare(ctx, a.Dir, d.Remote(repo), base, d.GitEnv); err != nil {
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
	exec, ok := d.Executors[cl.Tool.Executor]
	if !ok {
		return Outcome{}, fmt.Errorf("agentd: no executor %s on this host", cl.Tool.Executor)
	}
	sp, err := exec(a)
	if err != nil {
		return Outcome{}, err
	}
	logf, err := os.OpenFile(filepath.Join(a.Out, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = logf.Close() }()
	sp.Stdout, sp.Stderr = logf, logf
	entry := Entry{AttemptID: cl.AttemptID, TaskID: cl.TaskID, AttemptNo: cl.AttemptNo, Epoch: cl.Epoch, Role: cl.Role, Repo: cl.Repo,
		Effect: cl.EffectClass, PushedRef: cl.PushedRef, WorkID: cl.WorkID, Workdir: a.Dir, Base: base, Phase: "launching"}
	if l.isLost() {
		return d.lost(ctx, a, ws, &entry)
	}
	// The group is recorded and the start reported before the executor is
	// released: an executor never runs unrecorded, nor before the platform
	// knows it started (review 163 A7).
	proc, err := Launch(d.Self, sp, func(pgid int, start string) error {
		entry.PGID, entry.Start, entry.Phase = pgid, start, "running"
		if perr := d.Registry.Put(entry); perr != nil {
			return perr
		}
		return d.retry(ctx, func() error { return d.Client.Start(ctx, cl.AttemptID, cl.Epoch, cl.Tool.Model, cl.Tool.Provider) })
	})
	if err != nil {
		if entry.PGID == 0 {
			return Outcome{}, err
		}
		if errors.Is(err, ErrFenced) || l.isLost() {
			return d.lost(ctx, a, ws, &entry)
		}
		return Outcome{EndHow: "exit", Conclusion: "none"}, errors.Join(err, d.end(ctx, entry, "exit"))
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

// supervise watches the executor under its lease and returns how it ended:
// exit, hard_limit (the task's limit from the release), cancelled (the
// platform asked it to stop), lease_lost, or daemon_restart (agentd shuts
// down). Whatever ends it ends its whole group.
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
	end := ""
	stop := func(why string) {
		if end == "" {
			end = why
			safego.Go("agentd-terminate", func() { Terminate(proc.PGID, grace) })
		}
	}
	for {
		select {
		case code := <-exited:
			if end == "" {
				end = "exit"
			}
			return end, code
		case <-hard:
			stop("hard_limit")
		case <-ctx.Done():
			stop("daemon_restart")
		case <-l.lost:
			stop("lease_lost")
		case <-l.stop:
			stop("cancelled")
		case <-sample.C:
			if end == "" {
				d.checkpoint(ctx, a, ws, entry)
			}
		}
	}
}

// results reads what the executor left: the report (verify.json for a
// verification) and the tail of its log.
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
	if b, err := os.ReadFile(filepath.Join(a.Out, r.report)); err == nil && len(b) > 0 && len(b) <= maxBody {
		r.reportBody = b
	}
	if b, err := os.ReadFile(filepath.Join(a.Out, "log.txt")); err == nil {
		if len(b) > logTailBytes {
			b = b[len(b)-logTailBytes:]
		}
		r.logTail = b
	}
	return r
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
// tail and, when the workspace changed, its patch against the base. The new
// sample's number is recorded here before anything is uploaded, so a retry
// of one sample reuses its number; the platform's confirmation is recorded
// after (review 142 R2). A failed sample is left for the next one.
func (d *Daemon) checkpoint(ctx context.Context, a *Attempt, ws *Workspace, entry *Entry) {
	entry.CheckpointSeq++
	if err := d.Registry.Put(*entry); err != nil {
		d.logf("agentd: checkpoint %s: %v", entry.AttemptID, err)
		return
	}
	var ids []string
	tooLarge := false
	if tail := a.results().logTail; len(tail) > 0 {
		if id, err := d.Client.PutArtifact(ctx, "log_tail", "text/plain; charset=utf-8", tail); err == nil {
			ids = append(ids, id)
		}
	}
	if ws != nil {
		if dirty, err := ws.Dirty(ctx); err == nil && dirty {
			if patch, err := ws.Diff(ctx); err == nil && len(patch) > patchLimit {
				tooLarge = true
			} else if err == nil && len(patch) > 0 {
				if id, err := d.Client.PutArtifact(ctx, "checkpoint_patch", "text/x-diff", patch); err == nil {
					ids = append(ids, id)
				}
			}
		}
	}
	if err := d.Client.Checkpoint(ctx, entry.AttemptID, entry.Epoch, entry.CheckpointSeq, ids, tooLarge); err != nil {
		d.logf("agentd: checkpoint %s #%d: %v", entry.AttemptID, entry.CheckpointSeq, err)
		return
	}
	entry.Confirmed = entry.CheckpointSeq
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
