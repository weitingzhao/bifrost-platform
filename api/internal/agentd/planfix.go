package agentd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan's report that ends in no fenced json block, or in one that does not
// read, is fixed in the same attempt: the executor's own session is asked
// once, right after it exited, for the block alone, and its reply is
// appended to the report. Otherwise the report goes back for a whole second
// round, about ten minutes and one attempt of the mission's budget (11-05:
// four plans needed one for that alone). What the block says is checked by
// the platform as before (store.ParsePlan); only whether it reads is looked
// at here.

// planReply is where a follow-up's After writes its reply, in the attempt's
// output; report.md stays the first report meanwhile.
const planReply = "plan-reply.md"

// planFixPrompt asks for the block alone, quoting why the report's was not
// taken.
func planFixPrompt(why error) string {
	return fmt.Sprintf("Your report was read for its plan, and it could not be taken: %s.\n\n"+
		"Reply with only the fenced ```json block of the plan, in the format your spec gives, and nothing else: "+
		"it is appended to your report as its end. Change no file.\n", why)
}

// planRun is what planFix needs of the attempt Run started.
type planRun struct {
	a        *Attempt
	ws       *Workspace
	entry    *Entry
	l        *lease
	sp       Spawn
	sibs     []string
	confined bool
	started  time.Time // when the first run was released
	log      io.Writer // log.txt
}

// planFix asks a plan attempt's session for the block its report lacks, when
// the attempt ended exit 0 with its group cleared and its lease held, and
// the executor can go on in the same session. It returns how the attempt
// ended: as it did, unless the follow-up was stopped, lost the lease or saw
// agentd shut down, which end it as they would have ended the first run. A
// follow-up cut by the hard limit is one that fixed nothing: the first run
// ended within it.
func (d *Daemon) planFix(ctx context.Context, r planRun, endHow string, code int) (string, int) {
	a, e := r.a, r.entry
	if a.Claim.Role != "plan" || endHow != "exit" || code != 0 || e.Group != GroupCleared ||
		r.l.isLost() || r.l.isStopped() || r.sp.FollowUp == nil {
		return endHow, code
	}
	first, err := readAtMost(filepath.Join(a.Out, "report.md"), maxBody)
	if err != nil || len(first) == 0 {
		return endHow, code
	}
	why := store.PlanBlock(first)
	if why == nil {
		return endHow, code
	}
	fend, fcode, reply, err := d.followUp(ctx, r, why)
	fixed := false
	if err == nil && fend == "exit" && fcode == 0 && len(reply) > 0 {
		fixed, err = d.appendReply(a, first, reply)
	}
	switch {
	case fixed:
		_, _ = fmt.Fprintf(r.log, "agentd: the plan's report had no block that reads (%v); its session was asked once for it: fixed\n", why)
	case err != nil:
		_, _ = fmt.Fprintf(r.log, "agentd: the plan's report had no block that reads (%v); its session was asked once for it: not fixed (%v)\n", why, err)
	default:
		_, _ = fmt.Fprintf(r.log, "agentd: the plan's report had no block that reads (%v); its session was asked once for it: not fixed (%s, code %d)\n", why, fend, fcode)
	}
	switch fend {
	case "cancelled", "lease_lost", "daemon_restart":
		return fend, fcode
	}
	return endHow, code
}

// followUp runs the follow-up in the first run's wall and pins, within what
// is left of the hard limit, and returns how it ended and its reply. An end
// of "" is a follow-up that never ran.
func (d *Daemon) followUp(ctx context.Context, r planRun, why error) (string, int, []byte, error) {
	a, e, l, cl := r.a, r.entry, r.l, r.a.Claim
	var left time.Duration
	if limit := hardLimit(cl); limit > 0 {
		if left = limit - time.Since(r.started); left <= 0 {
			return "", 0, nil, errors.New("no time is left of the hard limit")
		}
	}
	sp, err := r.sp.FollowUp(planFixPrompt(why))
	if err != nil {
		return "", 0, nil, err
	}
	if a.Walled {
		sp.Argv = append(wallArgv(d.Self, executorWrites(cl.Tool.Executor, a, r.sibs)), sp.Argv...)
	}
	if r.confined {
		if err = checkPins(d.Wall, cl.Tool.Executor, d.Self, a, r.sibs, sp); err != nil {
			return "", 0, nil, err
		}
	}
	sp.Stdout, sp.Stderr = r.log, r.log
	// The new group is recorded before it is released, as the first was; the
	// platform knows the attempt started already.
	proc, err := Launch(d.Self, sp, func(pgid int, start string) error {
		e.PGID, e.Start, e.Phase, e.Group = pgid, start, "running", ""
		if perr := d.Registry.Put(*e); perr != nil {
			return perr
		}
		if l.isLost() || l.isStopped() {
			return errors.New("agentd: the lease was lost or the attempt stopped before the follow-up's release")
		}
		return nil
	})
	if err != nil {
		if e.Group == "" {
			d.clear(e, time.Now()) // the launcher, killed before its release
		}
		switch {
		case l.isLost():
			return "lease_lost", -1, nil, err
		case l.isStopped():
			return "cancelled", -1, nil, err
		}
		return "", 0, nil, err
	}
	fend, fcode := d.supervise(ctx, a, proc, r.ws, e, l, left)
	if sp.After != nil {
		if err = sp.After(); err != nil {
			return fend, fcode, nil, err
		}
	}
	reply, err := readAtMost(filepath.Join(a.Out, planReply), maxBody)
	if err == nil && len(reply) == 0 {
		err = errors.New("empty")
	}
	if err != nil {
		return fend, fcode, nil, fmt.Errorf("no reply: %w", err)
	}
	return fend, fcode, reply, nil
}

// appendReply writes report.md as the first report, a blank line and the
// reply, when that ends in a block that reads and fits what is read of a
// report; otherwise report.md stays the first report.
func (d *Daemon) appendReply(a *Attempt, first, reply []byte) (bool, error) {
	both := append(append(append([]byte{}, first...), "\n\n"...), reply...)
	if err := store.PlanBlock(both); err != nil {
		return false, fmt.Errorf("the reply: %w", err)
	}
	if len(both) > maxBody {
		return false, errors.New("the report with the reply is over what is read of a report")
	}
	root, err := ownDirAt(a.root, a.Out)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	if err = writeOwn(root, "report.md", both); err != nil {
		return false, err
	}
	_ = root.Remove(planReply)
	return true, nil
}
