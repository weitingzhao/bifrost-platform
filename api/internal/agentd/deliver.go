package agentd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Keeping the lease and handing the result over (design v5 §5.2 心跳与死人开关、
// 收尾; review 163 A3–A5, A7).
//
// The lease is kept from the claim to the hand-over: a heartbeat every Beat,
// each with its own short deadline. The platform saying the lease is not ours
// (409), or MaxFails failures in a row, marks it lost; the running executor's
// group is then ended and the result goes only to salvage.
//
// The hand-over is a journal in the registry: each phase's request is written
// there before it is sent (the exit and conclusion before computed; the
// report, artifacts and the commit before persist), so a restarted agentd
// resends the same request, and pushes the same commit, from where it
// stopped. A platform that cannot be reached leaves the journal as it is and
// the attempt pending, never forgotten: only a submitted, refused or salvaged
// attempt is ended and forgotten.

// ErrPending: the platform could not be reached; the attempt's journal is
// kept and the next takeover goes on with it.
var ErrPending = errors.New("agentd: the hand-over waits for the platform")

// lease keeps one attempt's lease.
type lease struct {
	lost, stop chan struct{}
	cancel     func()
	lostOnce   sync.Once
	stopOnce   sync.Once
	ended      sync.Once
}

func (l *lease) markLost() { l.lostOnce.Do(func() { close(l.lost) }) }
func (l *lease) markStop() { l.stopOnce.Do(func() { close(l.stop) }) }

func (l *lease) isLost() bool {
	select {
	case <-l.lost:
		return true
	default:
		return false
	}
}

// release stops the heartbeats.
func (l *lease) release() { l.ended.Do(l.cancel) }

// keepLease heartbeats the attempt until released.
func (d *Daemon) keepLease(ctx context.Context, attemptID string, epoch int64) *lease {
	ctx, cancel := context.WithCancel(ctx)
	l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: cancel}
	every := orDefault(d.Beat, 30*time.Second)
	timeout := every
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	maxFails := d.MaxFails
	if maxFails <= 0 {
		maxFails = 3
	}
	safego.Go("agentd-lease", func() {
		t := time.NewTicker(every)
		defer t.Stop()
		fails := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			hctx, hcancel := context.WithTimeout(ctx, timeout)
			stop, _, err := d.Client.Heartbeat(hctx, attemptID, epoch)
			hcancel()
			switch {
			case ctx.Err() != nil:
				return
			case errors.Is(err, ErrFenced):
				l.markLost()
				return
			case err != nil:
				fails++
				d.logf("agentd: heartbeat %s: %v (%d in a row)", attemptID, err, fails)
				if fails >= maxFails {
					l.markLost()
					return
				}
			case stop:
				fails = 0
				l.markStop()
			default:
				fails = 0
			}
		}
	})
	return l
}

// deliver hands the result of an attempt that kept its lease over, phase by
// phase from where its journal stands: exited → computed → persisted →
// submitted.
func (d *Daemon) deliver(ctx context.Context, a *Attempt, ws *Workspace, e *Entry, l *lease) (Outcome, error) {
	cl := a.Claim
	out := Outcome{EndHow: e.EndHow, Conclusion: e.Conclusion}
	if e.ExitCode != nil {
		out.ExitCode = *e.ExitCode
	}
	for {
		if l != nil && l.isLost() && e.Phase != "submitted" {
			return d.lost(ctx, a, ws, e)
		}
		var err error
		switch e.Phase {
		case "exited":
			err = d.retry(ctx, func() error {
				return d.Client.Computed(ctx, cl.AttemptID, cl.Epoch, e.ExitCode, e.EndHow, e.Conclusion)
			})
			if err == nil {
				e.Phase = "computed"
			}
		case "computed":
			if err = d.prepare(ctx, a, ws, e); err == nil {
				err = d.pushPrepared(ctx, ws, e)
			}
			if err == nil {
				err = d.retry(ctx, func() error { return d.Client.Persist(ctx, cl.AttemptID, cl.Epoch, e.result()) })
			}
			if err == nil {
				e.Phase = "persisted"
			}
		case "persisted":
			if l != nil {
				l.release() // a submitted attempt is no longer live; its heartbeats would be refused
			}
			err = d.retry(ctx, func() error { return d.Client.Submit(ctx, cl.AttemptID, cl.Epoch) })
			if err == nil {
				e.Phase = "submitted"
			}
		case "submitted":
			out.Submitted = true
			return out, d.end(ctx, *e, e.EndHow)
		default:
			return out, fmt.Errorf("agentd: %s has no hand-over phase %q", e.AttemptID, e.Phase)
		}
		switch {
		case err == nil:
			if perr := d.Registry.Put(*e); perr != nil {
				return out, perr
			}
		case errors.Is(err, ErrFenced):
			return d.lost(ctx, a, ws, e)
		case errors.Is(err, ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return out, fmt.Errorf("%w: %s at %s: %v", ErrPending, e.AttemptID, e.Phase, err)
		default:
			d.logf("agentd: %s refused at %s: %v", e.AttemptID, e.Phase, err)
			return out, d.end(ctx, *e, e.EndHow)
		}
	}
}

// prepare uploads the report and the log tail and makes the attempt's commit,
// once: the result is written to the journal before it is sent, so a
// restart sends the same one.
func (d *Daemon) prepare(ctx context.Context, a *Attempt, ws *Workspace, e *Entry) error {
	if e.Prepared {
		return nil
	}
	r := a.results()
	if r.logTail != nil {
		id, err := d.putRetried(ctx, "log_tail", "text/plain; charset=utf-8", r.logTail)
		if err != nil && !errors.Is(err, ErrUnavailable) {
			d.logf("agentd: log tail of %s refused: %v", e.AttemptID, err)
		} else if err != nil {
			return err
		} else {
			e.Artifacts = append(e.Artifacts, id)
		}
	}
	if r.reportBody != nil {
		media := "text/markdown; charset=utf-8"
		if r.report == "verify.json" {
			media = "application/json"
		}
		id, err := d.putRetried(ctx, r.reportKind, media, r.reportBody)
		switch {
		case err == nil:
			e.Report = id
		case errors.Is(err, ErrUnavailable):
			return err
		default:
			d.logf("agentd: report of %s refused: %v", e.AttemptID, err) // H8: no report
		}
	}
	if ws != nil && e.Effect == "branch" {
		cid, err := NewChangeID()
		if err != nil {
			return err
		}
		work := e.WorkID
		if work == "" {
			work = "unassigned"
		}
		commit, err := ws.Commit(ctx, fmt.Sprintf("agentrt: %s attempt %d", e.TaskID, e.AttemptNo), Lineage{Task: e.TaskID, ChangeID: cid, Work: work})
		if err != nil {
			return err
		}
		if commit == "" {
			commit = ws.Base // nothing changed: the attempt's branch is its base
		}
		e.PushedCommit, e.ChangeID = commit, cid
	}
	e.Prepared = true
	return d.Registry.Put(*e)
}

// pushPrepared pushes the journal's commit to the attempt's branch (again,
// after a restart: the same commit).
func (d *Daemon) pushPrepared(ctx context.Context, ws *Workspace, e *Entry) error {
	if ws == nil || e.PushedCommit == "" {
		return nil
	}
	var head string
	err := d.retry(ctx, func() error {
		var perr error
		head, perr = ws.Push(ctx, d.Remote(e.Repo), e.PushedCommit, e.PushedRef)
		if perr != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, perr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if head != e.PushedCommit {
		return fmt.Errorf("agentd: %s is at %s after pushing %s", e.PushedRef, head, e.PushedCommit)
	}
	return nil
}

// result is the journal's result as persist and salvage take it.
func (e *Entry) result() Result {
	r := Result{Report: e.Report, Artifacts: e.Artifacts}
	if e.PushedCommit != "" {
		r.PushedRef, r.PushedCommit = e.PushedRef, e.PushedCommit
	}
	return r
}

// lost hands over, by salvage only, what an attempt whose lease is gone left
// here, retrying while the platform says it is still in grace or cannot be
// reached.
func (d *Daemon) lost(ctx context.Context, a *Attempt, ws *Workspace, e *Entry) (Outcome, error) {
	out := Outcome{EndHow: "lease_lost", Conclusion: "none"}
	if e.EndHow == "" {
		e.EndHow = "lease_lost"
	}
	e.Phase = "lost"
	if err := d.Registry.Put(*e); err != nil {
		return out, err
	}
	if err := d.prepare(ctx, a, ws, e); err != nil {
		return out, fmt.Errorf("%w: %s: %v", ErrPending, e.AttemptID, err)
	}
	if err := d.pushPrepared(ctx, ws, e); err != nil {
		d.logf("agentd: %s not pushed: %v", e.AttemptID, err)
	}
	every := orDefault(d.SalvageEvery, 60*time.Second)
	for {
		err := d.Client.Salvage(ctx, e.AttemptID, e.result())
		if err == nil {
			out.Salvaged = true
			break
		}
		if !errors.Is(err, ErrInGrace) && !errors.Is(err, ErrUnavailable) {
			d.logf("agentd: salvage %s refused: %v", e.AttemptID, err)
			break
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(every):
		}
	}
	return out, d.end(ctx, *e, "lease_lost")
}

// end tells the platform the attempt is over on this host, and whether its
// process group is really gone, and forgets it.
func (d *Daemon) end(ctx context.Context, entry Entry, endHow string) error {
	cleared := true
	if entry.PGID > 1 && GroupAlive(entry.PGID) {
		if entry.Start != "" && Alive(entry.PGID, entry.Start) {
			Terminate(entry.PGID, orDefault(d.KillGrace, 10*time.Second))
		}
		cleared = !GroupAlive(entry.PGID)
	}
	if endHow == "" {
		endHow = "exit"
	}
	err := d.retry(ctx, func() error { return d.Client.Ended(ctx, entry.AttemptID, endHow, cleared) })
	if err != nil && (errors.Is(err, ErrUnavailable) || ctx.Err() != nil) {
		return fmt.Errorf("%w: %s not ended: %v", ErrPending, entry.AttemptID, err)
	}
	if err != nil {
		d.logf("agentd: ended %s refused: %v", entry.AttemptID, err)
	}
	return d.Registry.Delete(entry.AttemptID)
}

// Recover takes over what is in the registry (design v5 §5.2 本地登记; T01):
// at start, and before each claim for attempts whose hand-over waited for
// the platform. A process group still running is ended and reported as
// daemon_restart, never run on; an attempt whose executor had exited goes on
// with its hand-over from its journal, under its lease again; one whose
// lease was lost goes on with salvage.
func (d *Daemon) Recover(ctx context.Context) error {
	entries, err := d.Registry.Load()
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		e := e
		a := &Attempt{Claim: &Claimed{TaskID: e.TaskID, AttemptID: e.AttemptID, AttemptNo: e.AttemptNo, Role: e.Role, Repo: e.Repo,
			EffectClass: e.Effect, PushedRef: e.PushedRef, WorkID: e.WorkID, Epoch: e.Epoch}, Dir: e.Workdir, Out: e.Workdir + ".out"}
		var ws *Workspace
		if e.Repo != "" {
			ws = &Workspace{Dir: e.Workdir, Base: e.Base, env: d.GitEnv}
		}
		switch e.Phase {
		case "running", "launching":
			if e.PGID > 1 && Alive(e.PGID, e.Start) {
				Terminate(e.PGID, orDefault(d.KillGrace, 10*time.Second))
			}
			d.checkpoint(ctx, a, ws, &e) // the last sample, then the end
			err = d.end(ctx, e, "daemon_restart")
		case "exited", "computed", "persisted", "submitted":
			l := d.keepLease(ctx, e.AttemptID, e.Epoch)
			_, err = d.deliver(ctx, a, ws, &e, l)
			l.release()
		case "lost":
			_, err = d.lost(ctx, a, ws, &e)
		default:
			err = d.end(ctx, e, "daemon_restart")
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("agentd: take over %s: %w", e.AttemptID, err))
		}
	}
	return errors.Join(errs...)
}
