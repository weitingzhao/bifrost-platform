package agentd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
)

// Keeping the lease and handing the result over (design v5 §5.2 心跳与死人开关、
// 收尾; reviews 163 A3–A7 and 164 A2–A5, A8).
//
// The lease is kept from the claim until the submit is taken: a heartbeat
// every Beat, each with its own short deadline. While the executor runs, the
// platform saying the lease is not ours (409), or MaxFails failures in a row,
// marks it lost: the executor's group is ended and the result goes only to
// salvage. Once the executor has exited the hand-over's own requests decide:
// each is fenced by the platform on its own, so a heartbeat refused after a
// submit was taken (its answer lost) does not turn a submitted attempt into a
// lost one; the submit is resent and the platform answers it again.
//
// The hand-over is a journal in the registry: each phase's request is written
// there before it is sent (the exit and conclusion before computed; the
// report, artifacts and the commit before persist; the phase submitting
// before the submit; the ended request before ended), so a restarted agentd
// resends the same request, and pushes the same commit, from where it
// stopped. A platform that cannot be reached, or a step cancelled, leaves the
// journal as it is and the attempt pending, never forgotten. An attempt is
// forgotten only once ended is taken and its process group is gone.

// ErrPending: the platform could not be reached; the attempt's journal is
// kept and the next takeover goes on with it.
var ErrPending = errors.New("agentd: the hand-over waits for the platform")

// transient: a step that may succeed when tried again.
func transient(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// lease keeps one attempt's lease.
type lease struct {
	lost, stop chan struct{}
	cancel     func()
	done       chan struct{} // closed when the keeper has stopped
	// handingOver: the executor has exited. Failed heartbeats no longer stop
	// the keeper: the hand-over's own requests say whether the lease is gone.
	handingOver atomic.Bool
	lostOnce    sync.Once
	stopOnce    sync.Once
	ended       sync.Once
	// tail, once the executor's log is open, gives each heartbeat the
	// actions found since the last one (W-31 2.3).
	tail atomic.Pointer[actionTail]
	// beat (one slot) makes the keeper's heartbeats and the last one after
	// the executor exits go one at a time (review 265 B12); the last one
	// gives up waiting for it when its time is up (review 266 B5).
	beat chan struct{}
	// noActions: the platform said it takes no actions; none are sent again
	// for this attempt.
	noActions atomic.Bool
}

func (l *lease) markLost() { l.lostOnce.Do(func() { close(l.lost) }) }
func (l *lease) markStop() { l.stopOnce.Do(func() { close(l.stop) }) }

func (l *lease) isLost() bool { return closed(l.lost) }

func (l *lease) isStopped() bool { return closed(l.stop) }

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// release stops the heartbeats and waits for one in flight to finish (review
// 164 A3).
func (l *lease) release() {
	l.ended.Do(func() {
		l.cancel()
		<-l.done
	})
}

// keepLease heartbeats the attempt until released.
func (d *Daemon) keepLease(ctx context.Context, attemptID string, epoch int64) *lease {
	ctx, cancel := context.WithCancel(ctx)
	l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: cancel, done: make(chan struct{}), beat: make(chan struct{}, 1)}
	every := orDefault(d.Beat, 30*time.Second)
	timeout := min(max(every, time.Second), 10*time.Second)
	maxFails := d.MaxFails
	if maxFails <= 0 {
		maxFails = 3
	}
	safego.Go("agentd-lease", func() {
		defer close(l.done)
		t := time.NewTicker(every)
		defer t.Stop()
		fails, lastRefused := 0, ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			select {
			case l.beat <- struct{}{}:
			case <-ctx.Done():
				return
			}
			hctx, hcancel := context.WithTimeout(ctx, timeout)
			stop, refused, err := d.heartbeat(hctx, attemptID, epoch, l, l.tail.Load().take(actions.MaxPerBeat, time.Now()))
			<-l.beat
			hcancel()
			if refused != "" && refused != lastRefused {
				d.logf("agentd: heartbeat %s: actions not kept: %s", attemptID, refused)
			}
			lastRefused = refused
			switch {
			case ctx.Err() != nil:
				return
			case errors.Is(err, ErrFenced):
				l.markLost()
				return
			case err != nil:
				fails++
				d.logf("agentd: heartbeat %s: %v (%d in a row)", attemptID, err, fails)
				if fails >= maxFails && !l.handingOver.Load() {
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

// lastActions reads what the executor wrote since the last read and sends it
// in one more heartbeat, so actions of its last seconds are not lost (review
// 265 B12). It is best effort, observation, never a reason to hold up the
// hand-over: the read, the wait for the keeper's turn and the send share one
// budget, and a lost lease ends it; once its turn comes the lease is looked
// at again, and a fenced answer marks the lease lost (review 266 B5, B7).
func (d *Daemon) lastActions(ctx context.Context, attemptID string, epoch int64, l *lease) {
	t := l.tail.Load()
	if t == nil || l.isLost() || l.noActions.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, lastActionsBudget)
	defer cancel()
	safego.Go("agentd-last-lost", func() {
		select {
		case <-l.lost:
			cancel()
		case <-ctx.Done():
		}
	})
	rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
	read := t.flush(rctx)
	rcancel()
	if !read {
		return
	}
	acts := t.take(actions.MaxPerBeat, time.Now())
	if len(acts) == 0 {
		return
	}
	select {
	case l.beat <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-l.beat }()
	if ctx.Err() != nil || l.isLost() || l.noActions.Load() {
		return
	}
	_, refused, err := d.heartbeat(ctx, attemptID, epoch, l, acts)
	if refused != "" {
		d.logf("agentd: last actions of %s not kept: %s", attemptID, refused)
	}
	switch {
	case errors.Is(err, ErrFenced):
		l.markLost()
		d.logf("agentd: last actions of %s: the lease is fenced", attemptID)
	case err != nil:
		d.logf("agentd: last actions of %s not sent: %v", attemptID, err)
	}
}

// heartbeat sends one heartbeat with acts, none once the platform said it
// takes none, and records that answer before the turn is given back, so the
// next heartbeat, the keeper's or the last, sees it (review 267 B7). The
// caller holds the beat.
func (d *Daemon) heartbeat(ctx context.Context, attemptID string, epoch int64, l *lease, acts []actions.Action) (bool, string, error) {
	if l.noActions.Load() {
		acts = nil // the platform takes none: they are dropped, the lease goes on
	}
	stop, _, refused, err := d.Client.Heartbeat(ctx, attemptID, epoch, acts)
	if strings.HasPrefix(refused, ActionsUnsupported) {
		l.noActions.Store(true)
	}
	return stop, refused, err
}

// lastActionsBudget bounds lastActions as a whole.
const lastActionsBudget = 5 * time.Second

// deliver hands the result of an attempt that kept its lease over, phase by
// phase from where its journal stands: exited → computed → persisted →
// submitting → submitted.
func (d *Daemon) deliver(ctx context.Context, a *Attempt, ws *Workspace, e *Entry, l *lease) (Outcome, error) {
	cl := a.Claim
	if l != nil {
		l.handingOver.Store(true)
	}
	out := Outcome{EndHow: e.EndHow, Conclusion: e.Conclusion}
	if e.ExitCode != nil {
		out.ExitCode = *e.ExitCode
	}
	for {
		var err error
		switch e.Phase {
		case "exited":
			err = d.checkChanges(ctx, ws, e)
			out.Conclusion = e.Conclusion // the check may have failed it (review 240 B6, 241)
			if err != nil {
				break
			}
			// What the attempt used, read from its output each time this phase
			// runs (again after a restart: the output is still there).
			m := measure(cl.Tool.Executor, e.Workdir+".out")
			err = d.retry(ctx, func() error {
				refused, err := d.Client.Computed(ctx, cl.AttemptID, cl.Epoch, e.ExitCode, e.EndHow, e.Conclusion, m)
				if refused != "" {
					d.logf("agentd: computed %s: usage not kept: %s", cl.AttemptID, refused)
				}
				return err
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
			e.Phase = "submitting" // the lease is kept while the submit is sent (review 164 A3)
		case "submitting":
			err = d.retry(ctx, func() error { return d.Client.Submit(ctx, cl.AttemptID, cl.Epoch) })
			if err == nil {
				e.Phase = "submitted"
			}
		case "submitted":
			if l != nil {
				l.release() // a submitted attempt is no longer live; its heartbeats would be refused
			}
			out.Submitted = true
			return out, d.end(ctx, e, e.EndHow)
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
		case errors.Is(err, ErrRegistry):
			return out, err // the journal cannot be kept: never ended as refused (review 171 B1)
		case transient(err):
			return out, fmt.Errorf("%w: %s at %s: %v", ErrPending, e.AttemptID, e.Phase, err)
		default:
			d.logf("agentd: %s refused at %s: %v", e.AttemptID, e.Phase, err)
			return out, d.end(ctx, e, e.EndHow)
		}
	}
}

// checkChanges fails an attempt for what its executor changed in the
// checkout (unwantedChanges), once, before the conclusion is reported: the
// result is journalled, and a check cut short (the daemon stopping) is left
// for the next pass, after a restart too (review 239 B3). A model's attempt
// that pushes no branch (a review) may change nothing; agentd's own
// deterministic check runs the spec's commands, and a journal from before
// the executor was recorded is held to the configuration rule alone. The
// reason goes into the log, whose tail the platform keeps.
func (d *Daemon) checkChanges(ctx context.Context, ws *Workspace, e *Entry) error {
	if e.Checked || ws == nil || e.Conclusion == "none" {
		return nil
	}
	readOnly := e.Effect != "branch" && e.Executor != "" && e.Executor != "exec:deterministic"
	why, err := unwantedChanges(ctx, ws, readOnly)
	if err != nil {
		return err
	}
	if why != "" {
		d.logf("agentd: %s: %s", e.AttemptID, why)
		if r, oerr := ownDirAt(d.Root, e.Workdir+".out"); oerr == nil {
			_ = appendOwn(r, "log.txt", fmt.Sprintf("\nagentd: the attempt is failed: %s\n", why))
			_ = r.Close()
		}
		e.Conclusion = "failed"
	}
	e.Checked = true
	return d.Registry.Put(*e)
}

// prepare uploads the report and the log tail and makes the attempt's commit,
// once: each is written to the journal when it is done, so a restart sends
// the same result. An upload that may succeed later (the platform away, the
// step cancelled) leaves the journal where it was; only a refusal for good is
// taken as no report (review 164 A4).
func (d *Daemon) prepare(ctx context.Context, a *Attempt, ws *Workspace, e *Entry) error {
	if e.Prepared {
		return nil
	}
	r := a.results()
	if !e.LogTailDone {
		if r.logTail != nil {
			id, err := d.putRetried(ctx, "log_tail", "text/plain; charset=utf-8", a.handText("log_tail", r.logTail))
			switch {
			case err == nil:
				e.Artifacts = append(e.Artifacts, id)
			case transient(err):
				return err
			default:
				d.logf("agentd: log tail of %s refused: %v", e.AttemptID, err)
			}
		}
		e.LogTailDone = true
		if err := d.Registry.Put(*e); err != nil {
			return err
		}
	}
	if !e.ReportDone {
		if r.reportBody != nil {
			media := "text/markdown; charset=utf-8"
			if r.report == "verify.json" {
				media = "application/json"
			}
			id, err := d.putRetried(ctx, r.reportKind, media, a.handText(r.reportKind, r.reportBody))
			switch {
			case err == nil:
				e.Report = id
			case transient(err):
				return err
			default:
				d.logf("agentd: report of %s refused: %v", e.AttemptID, err) // H8: no report
			}
		}
		e.ReportDone = true
		if err := d.Registry.Put(*e); err != nil {
			return err
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
// after a restart: the same commit). A push that fails is a step to try
// again, not a refusal.
func (d *Daemon) pushPrepared(ctx context.Context, ws *Workspace, e *Entry) error {
	if ws == nil || e.PushedCommit == "" {
		return nil
	}
	var head string
	err := d.retry(ctx, func() error {
		var perr error
		head, perr = ws.Push(ctx, d.pushRemote(e.Repo), e.PushedCommit, e.PushedRef)
		if perr != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, perr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if head != e.PushedCommit {
		return fmt.Errorf("%w: %s is at %s after pushing %s", ErrUnavailable, e.PushedRef, head, e.PushedCommit)
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
// here: the result is prepared and its branch pushed first, and salvage is
// asked only then (review 164 A5). One try per call: a platform that says the
// attempt is still in grace, or cannot be reached, leaves it pending for the
// next takeover (review 164 A8).
func (d *Daemon) lost(ctx context.Context, a *Attempt, ws *Workspace, e *Entry) (Outcome, error) {
	out := Outcome{EndHow: "lease_lost", Conclusion: "none"}
	if e.Phase != "lost" {
		if e.EndHow == "" {
			e.EndHow = "lease_lost"
		}
		e.Phase = "lost"
		if err := d.Registry.Put(*e); err != nil {
			return out, err
		}
	}
	if err := d.prepare(ctx, a, ws, e); err != nil {
		return out, fmt.Errorf("%w: %s: %w", ErrPending, e.AttemptID, err) // ErrRegistry stays visible (review 171 B1)
	}
	if err := d.pushPrepared(ctx, ws, e); err != nil {
		return out, fmt.Errorf("%w: %s: the branch is not pushed: %v", ErrPending, e.AttemptID, err)
	}
	err := d.retry(ctx, func() error { return d.Client.Salvage(ctx, e.AttemptID, e.result()) })
	switch {
	case err == nil:
		out.Salvaged = true
	case errors.Is(err, ErrInGrace), transient(err):
		return out, fmt.Errorf("%w: %s: salvage: %v", ErrPending, e.AttemptID, err)
	default:
		d.logf("agentd: salvage %s refused: %v", e.AttemptID, err)
	}
	return out, d.end(ctx, e, "lease_lost")
}

// clear ends what is left of the entry's group (died: when its leader was
// seen to die; zero when not known).
func (d *Daemon) clear(e *Entry, died time.Time) {
	var pids []int
	e.Group, pids = ClearGroup(e.PGID, e.Start, died, orDefault(d.KillGrace, 10*time.Second))
	if e.Group != GroupCleared {
		d.logf("agentd: the process group %d of %s is not gone (%s; processes %v): end them by hand; this host takes no new work until they are",
			e.PGID, e.AttemptID, e.Group, pids)
	}
}

// end tells the platform the attempt is over on this host, and whether its
// process group is gone. The request is written before it is first sent and
// resent as it is; once it is taken the entry is forgotten if the group is
// gone, and kept as uncleared while it is not (review 164 A2).
func (d *Daemon) end(ctx context.Context, e *Entry, endHow string) error {
	if e.EndRequest == nil {
		if e.Group != GroupCleared {
			d.clear(e, time.Time{})
		}
		if endHow == "" {
			endHow = "exit"
		}
		e.EndRequest = &EndRequest{EndHow: endHow, Cleared: e.Group == GroupCleared}
		e.Phase = "ending"
		if err := d.Registry.Put(*e); err != nil {
			return err
		}
	}
	err := d.retry(ctx, func() error { return d.Client.Ended(ctx, e.AttemptID, e.EndRequest.EndHow, e.EndRequest.Cleared) })
	if transient(err) {
		return fmt.Errorf("%w: %s not ended: %v", ErrPending, e.AttemptID, err)
	}
	if err != nil {
		d.logf("agentd: ended %s refused: %v", e.AttemptID, err)
	}
	return d.settle(e)
}

// settle forgets an ended attempt whose group is gone and keeps one whose
// group is not.
func (d *Daemon) settle(e *Entry) error {
	if e.Group != GroupCleared {
		d.clear(e, time.Time{})
	}
	if e.Group == GroupCleared {
		// Every end comes here with its group gone: cursor's data directory,
		// outside the attempt's output, goes before its entry (review 361 B1).
		removeCursorData(d.Root, e.Workdir+".out", d.logf)
		return d.Registry.Delete(e.AttemptID)
	}
	e.Phase = "uncleared"
	return d.Registry.Put(*e)
}

// Blocked names an attempt whose process group is not known to be gone, or
// an exam run's record that still names its group (or does not read); the
// host takes no new work while there is one (review 164 A2, 417 A1).
func (d *Daemon) Blocked() (string, error) {
	entries, err := d.Registry.Load()
	if err != nil {
		return "", err
	}
	for _, id := range sortedIDs(entries) {
		if g := entries[id].Group; g == GroupRemains || g == GroupUnknown {
			return id, nil
		}
	}
	if left := examRecordsLeft(d.Root); len(left) > 0 {
		return "the exam run recorded in " + left[0], nil
	}
	return "", nil
}

func sortedIDs(m map[string]Entry) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Recover takes over what is in the registry (design v5 §5.2 本地登记; T01):
// at start, and before each claim. First every process group a previous run
// left is ended, all of them, before anything is sent to the platform; a
// group whose state was not known is looked at again (review 164 A8). Then
// each hand-over goes on from its journal, a bounded step each: one that
// waits for the platform stays in the registry, is tried again after
// SalvageEvery, and holds up neither the others nor the next claim: each
// step runs within TakeOverLimit, so a push that never ends is cut off and
// left pending (review 165 A3). It returns an error only when the registry
// cannot be used (review 165 B1). An attempt this process runs now is its
// Run's, and is left out (2.4 slice ③).
func (d *Daemon) Recover(ctx context.Context) error {
	// Read and sorted under the hold lock: a Run that writes its entry and
	// lets go of its attempt does both before this read, or after it with the
	// attempt still held (review 276 A1). An entry read here while held is
	// left out; one not held is no Run's any more, and nothing else writes it.
	d.mu.Lock()
	entries, err := d.Registry.Load()
	if d.afterLoad != nil {
		d.afterLoad()
	}
	var ids []string
	if err == nil {
		for _, id := range sortedIDs(entries) {
			if !d.live[id] {
				ids = append(ids, id)
			}
		}
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	// What an exam sitting cut short left running is ended as well, before
	// anything is sent; one not known gone blocks the host (Blocked; review
	// 417 A1).
	if why := ExamRunsLeft(d.Root, orDefault(d.KillGrace, 10*time.Second)); why != nil {
		d.logf("%v; this host takes no new work until then", why)
	}
	for _, id := range ids {
		e := entries[id]
		switch {
		case e.Phase == "running" || e.Phase == "launching":
			d.clear(&e, time.Time{})
			e.Phase, e.EndHow = "restarted", "daemon_restart"
		case e.Group == GroupRemains || e.Group == GroupUnknown:
			d.clear(&e, time.Time{})
		default:
			continue
		}
		if e.Phase == "uncleared" && e.Group == GroupCleared {
			removeCursorData(d.Root, e.Workdir+".out", d.logf)
			delete(entries, id)
			if err := d.Registry.Delete(id); err != nil {
				return err
			}
			continue
		}
		entries[id] = e
		if err := d.Registry.Put(e); err != nil {
			return err
		}
	}
	if d.next == nil {
		d.next = map[string]time.Time{}
	}
	for _, id := range ids {
		e, ok := entries[id]
		if !ok || e.Phase == "uncleared" || time.Now().Before(d.next[id]) {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, orDefault(d.TakeOverLimit, 5*time.Minute))
		err := d.takeOver(tctx, &e)
		cancel()
		if errors.Is(err, ErrRegistry) {
			return err
		}
		if err != nil {
			d.next[id] = time.Now().Add(orDefault(d.SalvageEvery, 60*time.Second))
			d.logf("agentd: take over %s: %v", id, err)
		} else {
			delete(d.next, id)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

// takeOver goes on with one entry's hand-over from its journal.
func (d *Daemon) takeOver(ctx context.Context, e *Entry) error {
	a := &Attempt{Claim: &Claimed{TaskID: e.TaskID, AttemptID: e.AttemptID, AttemptNo: e.AttemptNo, Role: e.Role, Repo: e.Repo,
		EffectClass: e.Effect, PushedRef: e.PushedRef, WorkID: e.WorkID, Epoch: e.Epoch}, Dir: e.Workdir, Out: e.Workdir + ".out"}
	var ws *Workspace
	if e.Repo != "" {
		ws = &Workspace{Dir: e.Workdir, Base: e.Base, env: isolatedGit(d.GitEnv), wall: d.gitWall(e.Workdir), root: d.Root}
	}
	if e.Group == GroupCleared && e.Phase != "launching" {
		afterExecutor(e.Executor, d.CodexHome, d.Root, a.Out, d.logf) // again, or for a run a restart cut short
	}
	var err error
	switch e.Phase {
	case "restarted":
		sctx, cancel := context.WithTimeout(ctx, orDefault(d.SampleLimit, 2*time.Minute))
		d.checkpoint(sctx, a, ws, e) // the last sample, then the end
		cancel()
		err = d.end(ctx, e, "daemon_restart")
	case "exited", "computed", "persisted", "submitting", "submitted":
		l := d.keepLease(ctx, e.AttemptID, e.Epoch)
		_, err = d.deliver(ctx, a, ws, e, l)
		l.release()
	case "lost":
		_, err = d.lost(ctx, a, ws, e)
	case "ending":
		err = d.end(ctx, e, "")
	default:
		err = d.end(ctx, e, "daemon_restart")
	}
	return err
}
