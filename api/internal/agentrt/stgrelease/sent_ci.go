package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7-2. The release CI selection passes over newer runs of P that have
// no verdict yet, so B5 may send P on the landing CI of the same commit while
// main's push CI is still running. That run stays the backstop. A red verdict
// before a send is the final gate's existing ci_red refusal. After a send
// nothing read P's CI again (DESIGN-B5-v3 §4.4); this check does, for every
// send: each try's latest permit (its started record, or the resend that
// replaced it), over every run of P however late it was created, for as long
// as main is still at P and at most sentCIHorizon after the send (see
// watchSentCI), and opens the same blocked{ci_red}
// (ci_results for an unusable run) the gate would have opened, marked as
// found after a send. Its effects are the
// existing ones: an Owner card, no further automatic STG release until a
// newer full-scope green CI of P, and a run already in flight is observed and
// verified as before. Only while main is still at P: once main has moved on,
// the refusal is handed to the later commit's own checks (see watchSentCI).
const (
	// sentCIHorizon bounds how long one send is watched, and what a restarted
	// process reads again. main's push CI ends long before it (about 16
	// minutes after the push, measured on 3e7b8e48).
	sentCIHorizon = 24 * time.Hour
	// sentCIEvery spaces the reads of one send; a pass reads one send. A send
	// still watched after sentCISlowAfter (main has stayed at P that long) is
	// read every sentCIEverySlow: each read lists every PipelineRun in cicd.
	sentCIEvery     = 2 * time.Minute
	sentCISlowAfter = time.Hour
	sentCIEverySlow = 10 * time.Minute
)

type sentCIWatch struct {
	read    time.Time
	settled bool
}

// sentTry is one send: a try that started, at its latest permit. A resend
// is a new permit and a retry a new try, so each is a send of its own (key).
type sentTry struct {
	key, id        string
	b              *store.StgBatch
	n, permit      int64
	p, run         string
	sent, accepted time.Time
	base           store.StgRecord
}

// sentTries lists the sends within the horizon. accepted is the newest
// creation time among every CI the batch accepted so far: a newer green a
// later try rested on already supersedes an older red (ci_red's own rule),
// and a block naming such a red could not be resolved by that green again.
// A try proven unsent never reached STG; a quarantined one may have.
func sentTries(s store.StgState, now time.Time) []sentTry {
	var out []sentTry
	for id, b := range s.Batches {
		var accepted time.Time
		for _, e := range b.CI {
			if at := object(e.Record, "ci").Time("created_at"); at.After(accepted) {
				accepted = at
			}
		}
		for n, t := range b.Tries {
			if t == nil || t.Started.Seq == 0 || t.Unsent.Seq > 0 || accepted.IsZero() {
				continue
			}
			permit := t.Permit
			if permit.Seq == 0 {
				permit = t.Started
			}
			x := sentTry{id: id, b: b, n: n, permit: permit.Seq, sent: permit.At, accepted: accepted,
				p: object(t.Snapshot.Record, "inputs").String("revision"), run: t.Started.Record.String("run"), base: object(t.Snapshot.Record, "base")}
			if x.p == "" || x.sent.IsZero() || now.Sub(x.sent) > sentCIHorizon {
				continue
			}
			x.key = fmt.Sprintf("%s/%d/%d", id, n, x.permit)
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (c *Controller) logNotice(msg string, args ...any) {
	if c.notice != nil {
		c.notice(msg, args...)
		return
	}
	slog.Warn(msg, args...)
}

// sentCIRefusal maps the newest decided run of P to the gate's own refusal,
// only when it is newer than every CI the batch rested on.
func sentCIRefusal(ci store.StgCIObservation, accepted time.Time) string {
	if ci.UID == "" || ci.CreatedAt.IsZero() || !ci.CreatedAt.After(accepted) {
		return ""
	}
	if ci.Status == "Failed" {
		return "ci_red"
	}
	if ci.Reason != "" {
		return "ci_results"
	}
	return ""
}

// mainHead reads main as B5 reads it. A read that fails, or answers with
// anything but a full commit (an empty ls-remote), is an error, never an
// answer: it is not taken as main having moved, and the send is read again
// after sentCIEvery.
func (c *Controller) mainHead(ctx context.Context) (string, error) {
	if c.Sources.Head == nil {
		return "", errors.New("STG main reader unavailable")
	}
	head, err := c.Sources.Head(ctx)
	if err != nil {
		return "", err
	}
	if !sampleSHA.MatchString(head) {
		return "", fmt.Errorf("STG main read returned %q, not a commit", head)
	}
	return head, nil
}

// watchSentCI reads at most one send per pass. It reports whether it wrote,
// so the pass ends on the changed stream.
func (c *Controller) watchSentCI(ctx context.Context, s store.StgState) (bool, error) {
	if c.Sources.SentCI == nil {
		return false, nil
	}
	now := c.now()
	if c.sent == nil {
		c.sent = map[string]*sentCIWatch{}
	}
	var next *sentTry
	current := map[string]bool{}
	for _, x := range sentTries(s, now) {
		current[x.key] = true
		w := c.sent[x.key]
		if w == nil {
			w = &sentCIWatch{}
			c.sent[x.key] = w
		}
		every := sentCIEvery
		if now.Sub(x.sent) > sentCISlowAfter {
			every = sentCIEverySlow
		}
		if w.settled || !w.read.IsZero() && now.Sub(w.read) < every {
			continue
		}
		if next == nil || w.read.Before(c.sent[next.key].read) {
			next = &x
		}
	}
	for key := range c.sent {
		if !current[key] {
			delete(c.sent, key)
		}
	}
	if next == nil {
		return false, nil
	}
	w := c.sent[next.key]
	w.read = now
	ci, err := c.Sources.SentCI(ctx, next.p)
	if err != nil {
		return false, err
	}
	why := sentCIRefusal(ci, next.accepted)
	if why == "" {
		// Settled only once main has moved on from P, with no run of P still
		// undecided. While main is at P a run of P may still be created and go
		// red, however late its webhook comes, and no run says which push made
		// it: a land/prod branch push of P runs the same CI, with the same
		// labels, as main's push (R5:5.7-2 pre-review). So no green settles a
		// send while main is at P; it is watched until main moves or the
		// horizon. Once main has moved on, a refusal of P is handed to the
		// later commit's checks (option A below): a run of P newer than every
		// decided one is still read to its verdict, nothing later is waited for.
		if ci.Undecided != 0 {
			return false, nil
		}
		head, err := c.mainHead(ctx)
		if err != nil {
			return false, err
		}
		w.settled = head != next.p
		return false, nil
	}
	// A refusal settles nothing while main is at P either: after a block a
	// rerun's green may resolve it and a run of P created later (main's push
	// CI, its webhook late) may still go red; that is a new run and a new
	// interval (R5:5.7-2 pre-review). main is read first, so every path below
	// knows whether main has moved.
	head, err := c.mainHead(ctx)
	if err != nil {
		return false, err
	}
	moved := head != next.p
	// One interval per rejected run: an interval that already named it, open
	// or resolved by the existing rules, is never opened again here. Reopening
	// would strand the stream: the green that resolved it is already recorded
	// against the old interval and cannot be used twice.
	for _, h := range s.BlockHistory {
		if object(object(h.Opened.Record, "detail"), "ci").String("uid") == ci.UID {
			w.settled = moved
			return false, nil
		}
	}
	// Only while main is still at P (coordinator 11-01, option A). Once main
	// has moved on to X, X's tree contains P, and B5 releases X only on a
	// full-scope green CI of X (landingci.ReadRelease sets console-scope
	// greens aside, Record refuses them): X's own landing CI when that ran
	// the full tests, otherwise X's full run on main. Either is the whole Go
	// suite on a tree containing P, and its final gate and the check after
	// its own send apply as here. So P's red is handed to X, as the final
	// gate hands a batch off when main moves (batch.go). A deterministic
	// failure, or a sibling ui/infra drift that is still there, turns X red
	// and blocks X; a flaky failure may not recur (DESIGN-B5-v3 amendment,
	// residual risk 6). Nothing enters the ledger then; a log line says so.
	// A read that fails opens nothing and is retried after sentCIEvery: it
	// never counts as main having moved.
	if moved {
		w.settled = true
		c.logNotice("STG post-send CI refusal not blocked: main has moved on, so the later commit's own checks decide",
			"P", next.p, "head", head, "why", why, "ci_run", ci.Run, "ci_uid", ci.UID, "batch", next.id, "try", next.n, "permit_seq", next.permit)
		return false, nil
	}
	base, known := store.StgTrustedBaseline(s)
	if !known {
		base = next.base
	}
	sent := map[string]any{"batch": next.id, "try": next.n, "permit_seq": next.permit, "run": next.run, "ci_run": ci.Run}
	r, err := c.blockedRecord(next.b, why, base, &ci, sent)
	if err != nil {
		return false, err
	}
	// One open interval per P, reason and base, as the final gate keeps it
	// (blockAndFinish): a different red of P found while one is open adds no
	// second card. Resolving the open one needs a newer full-scope green of P,
	// which is newer than this run too.
	for _, block := range s.Blocks {
		prior := block.Opened.Record
		if block.IsOpen() && prior.String("P") == r.String("P") && prior.String("why") == r.String("why") && object(prior, "base").String("P") == object(r, "base").String("P") {
			return false, nil
		}
	}
	if !c.configured() {
		return false, nil
	}
	ok, err := c.ledger().Commit(ctx, s.LastSeq, []store.StgRecord{r})
	if err != nil || !ok {
		w.read = time.Time{} // a lost race or failed commit is read again next pass
		return false, err
	}
	return true, nil
}
