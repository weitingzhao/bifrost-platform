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
	Dir     string    // the workspace
	Out     string    // where the executor writes report.md or verify.json
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

	Beat         time.Duration // heartbeat; 30 s
	MaxFails     int           // heartbeats failing in a row before the lease counts as lost; 3
	KillGrace    time.Duration // TERM to KILL; 10 s
	SalvageEvery time.Duration // salvage retry while in grace; 60 s
	Retry        time.Duration // pause between retries of a step the platform could not take; 5 s
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
	entry := Entry{AttemptID: cl.AttemptID, TaskID: cl.TaskID, Epoch: cl.Epoch, Workdir: a.Dir, Phase: "running"}
	proc, err := Launch(d.Self, sp, func(pgid int, start string) error {
		entry.PGID, entry.Start = pgid, start
		return d.Registry.Put(entry)
	})
	if err != nil {
		return Outcome{}, err
	}
	if err = d.retry(ctx, func() error { return d.Client.Start(ctx, cl.AttemptID, cl.Epoch, cl.Tool.Model, cl.Tool.Provider) }); err != nil {
		Terminate(proc.PGID, orDefault(d.KillGrace, 10*time.Second))
		_ = proc.Wait()
		return d.lost(ctx, a, ws, entry, "lease_lost")
	}

	endHow, code := d.supervise(ctx, a, proc)
	if endHow == "lease_lost" {
		return d.lost(ctx, a, ws, entry, endHow)
	}
	return d.finish(ctx, a, ws, entry, endHow, code)
}

// supervise keeps the lease while the executor runs and returns how it ended.
func (d *Daemon) supervise(ctx context.Context, a *Attempt, proc *Proc) (string, int) {
	cl := a.Claim
	exited := make(chan int, 1)
	safego.Go("agentd-attempt-wait", func() { exited <- proc.Wait() })
	beat := time.NewTicker(orDefault(d.Beat, 30*time.Second))
	defer beat.Stop()
	var hard <-chan time.Time
	if cl.HardLimitS > 0 {
		t := time.NewTimer(time.Duration(cl.HardLimitS) * time.Second)
		defer t.Stop()
		hard = t.C
	}
	maxFails := d.MaxFails
	if maxFails <= 0 {
		maxFails = 3
	}
	grace := orDefault(d.KillGrace, 10*time.Second)
	fails := 0
	end := ""
	for {
		select {
		case code := <-exited:
			if end == "" {
				end = "exit"
			}
			return end, code
		case <-hard:
			if end == "" {
				end = "hard_limit"
				Terminate(proc.PGID, grace)
			}
		case <-ctx.Done():
			if end == "" {
				end = "daemon_restart"
				Terminate(proc.PGID, grace)
			}
		case <-beat.C:
			if end != "" {
				continue
			}
			stop, _, err := d.Client.Heartbeat(ctx, cl.AttemptID, cl.Epoch)
			switch {
			case errors.Is(err, ErrFenced):
				end = "lease_lost"
			case err != nil:
				fails++
				d.logf("agentd: heartbeat %s: %v (%d in a row)", cl.AttemptID, err, fails)
				if fails >= maxFails {
					end = "lease_lost"
				}
			case stop:
				end = "cancelled"
			default:
				fails = 0
			}
			if end != "" {
				Terminate(proc.PGID, grace)
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
	}
	return "done"
}

// upload puts the report and the log tail; a report the platform refuses
// (H8: a local path in it) is left out, and the attempt has no report.
func (d *Daemon) upload(ctx context.Context, r results) Result {
	var res Result
	if r.logTail != nil {
		if id, err := d.putRetried(ctx, "log_tail", "text/plain; charset=utf-8", r.logTail); err == nil {
			res.Artifacts = append(res.Artifacts, id)
		} else {
			d.logf("agentd: log tail not uploaded: %v", err)
		}
	}
	if r.reportBody != nil {
		media := "text/markdown; charset=utf-8"
		if strings.HasSuffix(r.report, ".json") {
			media = "application/json"
		}
		if id, err := d.putRetried(ctx, r.reportKind, media, r.reportBody); err == nil {
			res.Report = id
		} else {
			d.logf("agentd: report not uploaded: %v", err)
		}
	}
	return res
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

// push commits the workspace's changes, if any, and pushes them to the
// attempt's branch.
func (d *Daemon) push(ctx context.Context, a *Attempt, ws *Workspace, res *Result) error {
	if ws == nil || a.Claim.EffectClass != "branch" {
		return nil
	}
	cid, err := NewChangeID()
	if err != nil {
		return err
	}
	work := a.Claim.WorkID
	if work == "" {
		work = "unassigned"
	}
	commit, err := ws.Commit(ctx, "agentrt: "+a.Claim.TaskID+" attempt "+fmt.Sprint(a.Claim.AttemptNo),
		Lineage{Task: a.Claim.TaskID, ChangeID: cid, Work: work})
	if err != nil || commit == "" {
		return err
	}
	var head string
	err = d.retry(ctx, func() error {
		var perr error
		head, perr = ws.Push(ctx, d.Remote(a.Claim.Repo), commit, a.Claim.PushedRef)
		return perr
	})
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("agentd: %s is at %s after pushing %s", a.Claim.PushedRef, head, commit)
	}
	res.PushedRef, res.PushedCommit = a.Claim.PushedRef, commit
	return nil
}

// finish hands over the result of an attempt that kept its lease.
func (d *Daemon) finish(ctx context.Context, a *Attempt, ws *Workspace, entry Entry, endHow string, code int) (Outcome, error) {
	cl := a.Claim
	r := a.results()
	out := Outcome{EndHow: endHow, ExitCode: code, Conclusion: a.conclusion(endHow, code, r)}
	exit := code
	if err := d.retry(ctx, func() error { return d.Client.Computed(ctx, cl.AttemptID, cl.Epoch, &exit, endHow, out.Conclusion) }); err != nil {
		return out, err
	}
	entry.Phase, entry.ExitCode, entry.EndHow = "computed", &exit, endHow
	if err := d.Registry.Put(entry); err != nil {
		return out, err
	}
	res := d.upload(ctx, r)
	if err := d.push(ctx, a, ws, &res); err != nil {
		d.logf("agentd: %s not pushed: %v", cl.AttemptID, err)
	}
	if err := d.retry(ctx, func() error { return d.Client.Persist(ctx, cl.AttemptID, cl.Epoch, res) }); err != nil {
		d.logf("agentd: %s not persisted: %v", cl.AttemptID, err)
	} else if err := d.retry(ctx, func() error { return d.Client.Submit(ctx, cl.AttemptID, cl.Epoch) }); err == nil {
		out.Submitted = true
	}
	return out, d.end(ctx, entry, endHow)
}

// lost hands over, by salvage only, what an attempt whose lease is gone
// left here, retrying while the platform says it is still in grace.
func (d *Daemon) lost(ctx context.Context, a *Attempt, ws *Workspace, entry Entry, endHow string) (Outcome, error) {
	out := Outcome{EndHow: endHow, Conclusion: "none"}
	entry.Phase, entry.EndHow = "lost", endHow
	if err := d.Registry.Put(entry); err != nil {
		return out, err
	}
	res := d.upload(ctx, a.results())
	if err := d.push(ctx, a, ws, &res); err != nil {
		d.logf("agentd: %s not pushed: %v", a.Claim.AttemptID, err)
	}
	every := orDefault(d.SalvageEvery, 60*time.Second)
	for {
		err := d.Client.Salvage(ctx, a.Claim.AttemptID, res)
		if err == nil {
			out.Salvaged = true
			break
		}
		if !errors.Is(err, ErrInGrace) && !errors.Is(err, ErrUnavailable) {
			d.logf("agentd: salvage %s: %v", a.Claim.AttemptID, err)
			break
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(every):
		}
	}
	return out, d.end(ctx, entry, endHow)
}

// end tells the platform the attempt is over on this host and forgets it.
func (d *Daemon) end(ctx context.Context, entry Entry, endHow string) error {
	if err := d.retry(ctx, func() error { return d.Client.Ended(ctx, entry.AttemptID, endHow, true) }); err != nil {
		return err
	}
	return d.Registry.Delete(entry.AttemptID)
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
