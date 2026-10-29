package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Landing grants, continued (landing_grant_test.go has the fixture): races,
// lost COMMIT answers, expiry, the push object, the wait, lock order, B5's
// read, the create check and the controller (§6 T31–T36, T38/T39, T48, T49).

// Serial: FreezeEventClock and a test hook. T31: revocation and consumption
// on two connections, each first in turn. Revoked first: the gate, which
// waits on the stream lock, finds no usable grant and pushes nothing.
// Consumed first: the revocation, which waits, answers "handed to the push"
// and writes nothing.
func TestC5T31RevokeAgainstConsume(t *testing.T) {
	t.Run("revoke first", func(t *testing.T) {
		c := c5Wait(t, c5Core, false, true)
		g := c.grant(t, store.ActionCoreLanding)
		held, release := make(chan struct{}), make(chan struct{})
		defer store.SetHook(func(point, arg string) {
			if point == "landing grant: revoke before commit" {
				close(held)
				<-release
			}
		})()
		done := make(chan error, 1)
		go func() { _, err := store.RevokeLandingGrant(ctx, c.w.rt, g.GrantID); done <- err }()
		<-held
		gate := make(chan landing.Decision, 1)
		go func() {
			_, d, err := store.AuthorizeLandingPush(ctx, c.w.db.Connect(t, c.w.db.Runtime), c.src, c.op, c.observe())
			if err != nil {
				t.Error(err)
			}
			gate <- d
		}()
		time.Sleep(200 * time.Millisecond)
		close(release)
		ok(t, <-done)
		if d := <-gate; d.Allowed || d.Reason != "awaiting_grant" || c.phase(t) != "awaiting_grant" {
			t.Fatalf("pushed a revoked grant: %+v", d)
		}
	})
	t.Run("consume first", func(t *testing.T) {
		c := c5Wait(t, c5Core, false, true)
		g := c.grant(t, store.ActionCoreLanding)
		held, release := make(chan struct{}), make(chan struct{})
		defer store.SetHook(func(point, arg string) {
			if point == "landing gate: before main_intent" {
				close(held)
				<-release
			}
		})()
		gate := make(chan landing.Decision, 1)
		go func() {
			_, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, c.src, c.op, c.observe())
			if err != nil {
				t.Error(err)
			}
			gate <- d
		}()
		<-held
		done := make(chan store.RevokeResult, 1)
		go func() {
			r, err := store.RevokeLandingGrant(ctx, c.w.db.Connect(t, c.w.db.Runtime), g.GrantID)
			if err != nil {
				t.Error(err)
			}
			done <- r
		}()
		time.Sleep(200 * time.Millisecond)
		close(release)
		if d := <-gate; !d.Allowed {
			t.Fatalf("gate: %+v", d)
		}
		if r := <-done; r.Revoked || !r.Consumed || r.IntentSeq == 0 {
			t.Fatalf("revocation after consumption: %+v", r)
		}
		if n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_grant_revoked'`); n != 0 {
			t.Fatal("wrote a revocation of a consumed grant")
		}
	})
}

// lostCommit is a DB whose next transaction's COMMIT fails: "after" commits
// then reports the failure, "before" rolls back then reports it. readback
// fails every transaction that follows.
type lostCommit struct {
	store.DB
	fault    string
	used     bool
	readback bool
}

func (l *lostCommit) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if l.used && l.readback {
		return nil, errors.New("readback connection lost")
	}
	tx, err := l.DB.BeginTx(ctx, o)
	if err != nil || l.used {
		return tx, err
	}
	return &lostCommitTx{Tx: tx, l: l}, nil
}

type lostCommitTx struct {
	pgx.Tx
	l *lostCommit
}

func (t *lostCommitTx) Commit(ctx context.Context) error {
	if t.l.used {
		return t.Tx.Commit(ctx)
	}
	t.l.used = true
	if t.l.fault == "after" {
		if err := t.Tx.Commit(ctx); err != nil {
			return err
		}
	} else {
		_ = t.Tx.Rollback(ctx)
	}
	return errors.New("connection lost at COMMIT")
}

// armed returns a lostCommit that faults the first writing transaction
// after skip read transactions pass untouched.
func armed(db store.DB, fault string, readback bool) *lostCommit {
	return &lostCommit{DB: db, fault: fault, readback: readback}
}

// Serial: FreezeEventClock. T32: a COMMIT whose answer was lost, for the
// executor's grant: written → the grant; not written → a failure (the
// approval can be requested again); the read back failing → uncertain.
// The pre-read transactions run on the plain connection.
func TestC5T32CreateUncertain(t *testing.T) {
	for _, tc := range []struct {
		fault    string
		readback bool
		want     string
	}{{"after", false, "written"}, {"before", false, "failed"}, {"after", true, "uncertain"}} {
		t.Run(tc.fault+map[bool]string{true: " readback lost", false: ""}[tc.readback], func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			p := c.params(t)
			db := &grantWriteFault{DB: c.w.rt, l: armed(c.w.rt, tc.fault, tc.readback)}
			_, err := store.WriteLandingGrant(ctx, db, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p)
			n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_grant'`)
			switch tc.want {
			case "written":
				if err != nil || n != 1 {
					t.Fatal(err, n)
				}
			case "failed":
				if err == nil || errors.Is(err, store.ErrGrantUncertain) || n != 0 {
					t.Fatal(err, n)
				}
			case "uncertain":
				if !errors.Is(err, store.ErrGrantUncertain) || n != 1 {
					t.Fatal(err, n)
				}
			}
		})
	}
}

// grantWriteFault faults only the writing transaction: WriteLandingGrant's
// third (pre-read, then its locked write; a main-moved path writes none).
type grantWriteFault struct {
	store.DB
	l     *lostCommit
	begun int
}

func (g *grantWriteFault) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	g.begun++
	if g.begun >= 2 {
		return g.l.BeginTx(ctx, o)
	}
	return g.DB.BeginTx(ctx, o)
}

// Serial: FreezeEventClock. T33: the same for a revocation (§5.7 table).
func TestC5T33RevokeUncertain(t *testing.T) {
	for _, tc := range []struct {
		fault    string
		readback bool
		want     string
	}{{"after", false, "revoked"}, {"before", false, "failed"}, {"after", true, "uncertain"}} {
		t.Run(tc.fault+map[bool]string{true: " readback lost", false: ""}[tc.readback], func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			g := c.grant(t, store.ActionCoreLanding)
			r, err := store.RevokeLandingGrant(ctx, armed(c.w.rt, tc.fault, tc.readback), g.GrantID)
			n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_grant_revoked'`)
			switch tc.want {
			case "revoked":
				if err != nil || !r.Revoked || n != 1 {
					t.Fatal(r, err, n)
				}
			case "failed":
				if err == nil || errors.Is(err, store.ErrGrantUncertain) || n != 0 {
					t.Fatal(err, n)
				}
				// It may be retried.
				if r, err = store.RevokeLandingGrant(ctx, c.w.rt, g.GrantID); err != nil || !r.Revoked {
					t.Fatal(r, err)
				}
			case "uncertain":
				if !errors.Is(err, store.ErrGrantUncertain) || n != 1 {
					t.Fatal(err, n)
				}
			}
		})
	}
}

// Serial: FreezeEventClock. T34: the consuming main_intent's COMMIT answer
// is lost: no Authorization comes back (so nothing pushes), the stream has
// the intent and the grant is consumed; the next gate does not authorize
// again. The reconciler owns it from here (AtMain).
func TestC5T34ConsumeUncertain(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	auth, _, err := store.AuthorizeLandingPush(ctx, armed(c.w.rt, "after", false), c.src, c.op, c.observe())
	if err == nil || auth.Op != "" {
		t.Fatal("an authorization came back from a lost commit", auth, err)
	}
	if c.phase(t) != "main_intent" {
		t.Fatal(c.phase(t))
	}
	usable, err := store.HasUsableLandingGrant(ctx, c.w.rt, c.op)
	ok(t, err)
	if auth, d := c.gate(t); usable || d.Allowed || auth.Op != "" {
		t.Fatal("authorized again", d)
	}
}

// Serial: FreezeEventClock. T35: the grant expires between the gate's first
// look and its decision (the reads took long): refused at the decision,
// nothing written. Control: TestC5GrantWrittenAndConsumed.
func TestC5T35ExpiryAtTheDecision(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	a := c.approval(store.ActionCoreLanding, store.LandingGrantTTL-time.Minute)
	_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, c.params(t))
	ok(t, err)
	slow := func(ctx context.Context, o landing.Operation) (store.LiveObservation, error) {
		store.FreezeEventClock(c.at.Add(2 * time.Minute))
		return c.observe()(ctx, o)
	}
	_, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, c.src, c.op, slow)
	ok(t, err)
	if d.Allowed || d.Reason != "grant_expired" || c.phase(t) != "awaiting_grant" || c.reservations(t) != 0 {
		t.Fatalf("%+v", d)
	}
}

// Serial: FreezeEventClock. T36: what is pushed is R itself. A candidate
// with another R (a trailer added after approval) has another digest even
// with the same patch; a grant for it is refused by the append check, and
// the gate's grant is the operation's R only.
func TestC5T36PushIsR(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	g := c.grant(t, store.ActionCoreLanding)
	auth, d := c.gate(t)
	if !d.Allowed || auth.R != g.R || auth.R != commitB {
		t.Fatal(d, auth, g)
	}
	var intentR string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->'landing'->>'R' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`,
		landingRepoStream).Scan(&intentR))
	if intentR != g.R {
		t.Fatal(intentR)
	}
	a := store.GrantCandidate{Env: "prod", R: commitB, PatchSHA256: strings.Repeat("7", 64)}
	b := a
	b.R = commitC
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da == db {
		t.Fatal("R is not in the candidate digest")
	}
}

// Serial: FreezeEventClock. T38/T39 on a real grant: a valid grant stops W
// (its interval is not waiting), revocation starts it again, from the
// stream alone (a fresh read is a restart, T43).
func TestC5GrantPausesTheWait(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	g := c.grant(t, store.ActionCoreLanding) // granted at c.at
	waited := func(at time.Time) time.Duration {
		store.FreezeEventClock(at)
		st, err := store.ReadLandingWait(ctx, c.w.rt, c.op)
		ok(t, err)
		return time.Duration(st.WaitingSeconds) * time.Second
	}
	before := waited(c.at)
	if w := waited(c.at.Add(5 * time.Hour)); w != before {
		t.Fatal("W ran under a valid grant", before, w)
	}
	store.FreezeEventClock(c.at.Add(5 * time.Hour))
	_, err := store.RevokeLandingGrant(ctx, c.w.rt, g.GrantID)
	ok(t, err)
	if w := waited(c.at.Add(6 * time.Hour)); w != before+time.Hour {
		t.Fatal("W did not resume after revocation", before, w)
	}
}

// Serial: FreezeEventClock and test hooks. T48 (review 694 R-07): B4's
// Close takes the mission (FOR NO KEY UPDATE) and stops before the
// repository stream; the executor then starts and must wait on the mission
// holding nothing Close needs, while the Owner revokes the first grant
// (the stream alone). Released, Close finishes, then the executor, whose
// grant follows the revocation: the serial result. An executor that took the
// stream before the mission would deadlock here (PostgreSQL ends one side:
// an error, red); the mutant was run by hand (C-5 fix report).
func TestC5T48LockOrder(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	first := c.grant(t, store.ActionCoreLanding)
	p := c.params(t)
	closeHeld, closeRelease := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer store.SetHook(func(point, arg string) {
		if point == "close: mission locked" && arg == "M-3" {
			once.Do(func() {
				close(closeHeld)
				<-closeRelease
			})
		}
	})()
	closed := make(chan error, 1)
	go func() { _, err := store.CloseMissions(ctx, c.w.db.Connect(t, c.w.db.Runtime)); closed <- err }()
	select {
	case <-closeHeld:
	case err := <-closed:
		t.Fatal("Close did not reach the mission lock", err)
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not reach the mission lock")
	}
	wrote := make(chan error, 1)
	go func() {
		_, err := store.WriteLandingGrant(ctx, c.w.db.Connect(t, c.w.db.Runtime), c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p)
		wrote <- err
	}()
	time.Sleep(300 * time.Millisecond) // the executor waits on the mission
	revokeDone := make(chan error, 1)
	go func() {
		r, err := store.RevokeLandingGrant(ctx, c.w.db.Connect(t, c.w.db.Runtime), first.GrantID)
		if err == nil && !r.Revoked {
			err = fmt.Errorf("not revoked: %+v", r)
		}
		revokeDone <- err
	}()
	// The revocation needs the stream alone: it must not wait behind an
	// executor that waits on the mission.
	var errs []error
	pending := 2
	select {
	case err := <-revokeDone:
		errs = append(errs, err)
	case <-time.After(5 * time.Second):
		errs = append(errs, errors.New("the revocation waits behind the executor: the executor holds the stream while waiting on the mission"))
		pending++
	}
	close(closeRelease)
	deadline := time.After(30 * time.Second)
	for ; pending > 0; pending-- {
		select {
		case err := <-wrote:
			errs = append(errs, err)
		case err := <-closed:
			errs = append(errs, err)
		case err := <-revokeDone:
			errs = append(errs, err)
		case <-deadline:
			t.Fatal("deadlock", errs)
		}
	}
	for _, err := range errs {
		ok(t, err)
	}
	var revoked, granted int
	ok(t, c.w.rt.QueryRow(ctx, `SELECT count(*) FILTER (WHERE body_json ? 'landing_grant_revoked'), count(*) FILTER (WHERE body_json ? 'landing_grant')
	  FROM agentrt.event WHERE event_stream_id=$1`, landingRepoStream).Scan(&revoked, &granted))
	g, err := store.UsableLandingGrant(ctx, c.w.rt, c.op)
	ok(t, err)
	if revoked != 1 || granted != 2 || g == "" || g == first.GrantID {
		t.Fatal("not the serial result", revoked, granted, g)
	}
}

// Serial: FreezeEventClock. T49: B5's read gives evidence only for an
// operation whose main_intent consumed its grant and that then landed R;
// not for an unconsumed or revoked grant, nor for another operation.
func TestC5T49B5Evidence(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	g := c.grant(t, store.ActionCoreLanding)
	evidence := func(op string) bool {
		t.Helper()
		var found bool
		ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
			_, f, err := store.LandingGrantEvidence(ctx, tx, "bifrost-platform", op)
			found = f
			return err
		}))
		return found
	}
	if evidence(c.op) {
		t.Fatal("evidence before consumption")
	}
	auth, d := c.gate(t)
	if !d.Allowed {
		t.Fatal(d)
	}
	if evidence(c.op) {
		t.Fatal("evidence before landed")
	}
	_, err := store.RecordPushResult(ctx, c.w.rt, auth, "ok")
	ok(t, err)
	effectiveRead(t, c.w, c.at.Add(time.Minute))
	if c.phase(t) != "landed" {
		t.Fatal(c.phase(t))
	}
	var ev store.GrantEvidence
	ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		var found bool
		var err error
		ev, found, err = store.LandingGrantEvidence(ctx, tx, "bifrost-platform", c.op)
		if !found {
			t.Fatal("no evidence for a landed grant")
		}
		return err
	}))
	if ev.GrantID != g.GrantID || ev.R != commitB || !ev.STGRelease || ev.Purpose != "landing_main" || ev.IntentSeq == 0 {
		t.Fatalf("%+v", ev)
	}
	if evidence("land:" + c.root + ":2") {
		t.Fatal("evidence for another op")
	}
	// A revoked, unconsumed grant: none.
	r := c5Wait(t, c5Core, false, true)
	rg := r.grant(t, store.ActionCoreLanding)
	_, err = store.RevokeLandingGrant(ctx, r.w.rt, rg.GrantID)
	ok(t, err)
	ok(t, store.InTx(ctx, r.w.rt, func(tx pgx.Tx) error {
		_, found, err := store.LandingGrantEvidence(ctx, tx, "bifrost-platform", r.op)
		if found {
			t.Fatal("evidence for a revoked grant")
		}
		return err
	}))
}

// Serial: FreezeEventClock. The create check's store half (§5.6 item 2):
// the request as handin.sh status prints it passes; main moved refuses it;
// a stale digest refuses it; a core change's review without the trust
// chain's answers refuses it (§12.2 item 3).
func TestC5CreateCheck(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	p := c.params(t)
	if _, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionCoreLanding, p); err != nil {
		t.Fatal(err)
	}
	c.main = commitC
	_, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionCoreLanding, p)
	refusedWith(t, err, "main_changed")
	c.main = commitA
	q := p
	q.MaterialDigest = "sha256:" + strings.Repeat("e", 64)
	_, err = store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionCoreLanding, q)
	refusedWith(t, err, "material_mismatch")
	_, err = store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionGuardrailLanding, p)
	refusedWith(t, err, "action_mismatch")
	// Without the trust review's answers: refused.
	d := c5WaitNoTrust(t)
	m, err := store.ReadLandingGrantMaterial(ctx, d.w.rt, d.gsrc, d.op, true)
	ok(t, err)
	if !slices.Contains(m.Problems, "trust_review_missing") {
		t.Fatal(m.Problems)
	}
	_, err = store.CheckLandingGrantRequest(ctx, d.w.rt, d.gsrc, store.ActionCoreLanding, store.GrantParams{Env: "prod", Repo: "bifrost-platform",
		Task: d.root, Op: d.op, R: commitB, MaterialDigest: m.MaterialDigest, STGRelease: true})
	refusedWith(t, err, "trust_review_missing")
}

// c5WaitNoTrust is c5Wait without the trust chain's answers: the latest
// passing review is the round's own report.
func c5WaitNoTrust(t *testing.T) *c5 {
	t.Helper()
	c := c5Wait(t, c5Core, false, true)
	ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		var attempt, reviewer string
		if err := tx.QueryRow(ctx, `SELECT attempt_id, verifier_attempt_id FROM agentrt.verification WHERE task_id=$1 AND method='review' ORDER BY created_at LIMIT 1`,
			c.root).Scan(&attempt, &reviewer); err != nil {
			return err
		}
		evidence, err := store.PutArtifact(ctx, tx, "controller", "review", "text/markdown", []byte("Looks right.\n"+c.root))
		if err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: a review without the trust answers"}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verifier_attempt_id, verdict, evidence_artifact_id, created_at)
		  VALUES ($1, $2, $3, 'review', $4, 'pass', $5, now() + interval '1 hour')`, store.NewVerificationID(), c.root, attempt, reviewer, evidence)
		return err
	}))
	return c
}

// Serial: FreezeEventClock. §5.5: the grant is usable only for the candidate
// the Owner saw. R's latest CI replaced by another run (green too), or R's
// patch read differently, makes it stale: nothing reserved or pushed, and
// the grant is spent (review 694 R-05). A new request on the evidence as
// it is now is granted and pushes (the control).
func TestC5GrantStaleOnOtherEvidence(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	ci := c.ci
	c.ci.Run, c.ci.UID, c.ci.CreatedAt = "green-again", "uid-again", ci.CreatedAt.Add(time.Minute)
	if _, d := c.gate(t); d.Allowed || d.Reason != "grant_stale" || c.reservations(t) != 0 || c.phase(t) != "awaiting_grant" {
		t.Fatalf("another CI run: %+v", d)
	}
	if st, _ := store.ReadLandingWait(ctx, c.w.rt, c.op); st.Shown != "grant_refused" || st.GrantRefused != "grant_stale" {
		t.Fatalf("status: %+v", st)
	}
	c.grant(t, store.ActionCoreLanding)
	if _, d := c.gate(t); !d.Allowed {
		t.Fatalf("a new grant on the new run: %+v", d)
	}
	// Another patch, on a fresh world.
	d := c5Wait(t, c5Core, false, true)
	d.grant(t, store.ActionCoreLanding)
	d.patch.SHA256 = strings.Repeat("8", 64)
	if _, dec := d.gate(t); dec.Allowed || dec.Reason != "grant_stale" || d.reservations(t) != 0 {
		t.Fatalf("another patch: %+v", dec)
	}
}

// Serial: FreezeEventClock. The controller's resume of a waiting operation
// (C-5): with live off nothing opens or pushes; with live on and no usable
// grant nothing is read outside the database; with its grant it pushes
// once, the patch read through DrillSources.GrantPatch; without a patch
// reader the gate refuses (T25 "live off" and the controller half of T27).
func TestC5ControllerResumesOnlyWithAGrant(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	p := &livePusher{outcome: landinggit.PushOK}
	opened := 0
	open := func(_ context.Context, r string) (controller.LiveWorkspace, error) {
		opened++
		if r != commitB {
			t.Fatal("workspace for", r)
		}
		return p, nil
	}
	patches := 0
	src := controller.DrillSources{Facts: c.src,
		Main:          func(context.Context) (string, error) { return c.main, nil },
		WindowBlocked: func(context.Context) (bool, error) { return false, nil },
		CI:            func(context.Context, string, time.Time) (landing.CIRun, error) { return c.ci, nil },
		EnsureCI:      func(context.Context, string, string) error { return nil }}
	src.Facts.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) {
		patches++
		return c.patch, nil
	}
	on := controller.Controller{DB: c.w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{}, Drill: &src, OpenLive: open}
	// No grant: the gate rechecks the rule once for this rule version
	// (R-06): core still fails, nothing is pushed, nor read a second time.
	ok(t, on.LandingDrillPass(ctx))
	ok(t, on.LandingDrillPass(ctx))
	if opened != 1 || patches != 0 || len(p.pushes) != 0 || c.phase(t) != "awaiting_grant" {
		t.Fatal("polled without a grant", opened, patches, p.pushes)
	}
	opened = 0
	c.grant(t, store.ActionCoreLanding)
	off := controller.Controller{DB: c.w.rt, Env: "prod", DrillGit: &branchObserved{}, Drill: &src, OpenLive: open}
	ok(t, off.LandingDrillPass(ctx))
	if opened != 0 || len(p.pushes) != 0 || c.phase(t) != "awaiting_grant" {
		t.Fatal("live off pushed", opened, p.pushes)
	}
	noPatch := src
	noPatch.Facts.GrantPatch = nil
	blind := controller.Controller{DB: c.w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{}, Drill: &noPatch, OpenLive: open}
	ok(t, blind.LandingDrillPass(ctx))
	if len(p.pushes) != 0 || c.phase(t) != "awaiting_grant" || opened != 1 {
		t.Fatal("pushed without the patch evidence", p.pushes, opened)
	}
	// A grant the gate could not judge is tried again only after a while.
	ok(t, blind.LandingDrillPass(ctx))
	if opened != 1 {
		t.Fatal("tried the grant again at once", opened)
	}
	ok(t, on.LandingDrillPass(ctx))
	if len(p.pushes) != 1 || p.pushes[0] != commitB+"<-"+commitA || patches != 1 {
		t.Fatal("pushes", p.pushes, patches)
	}
	if ph := c.phase(t); ph != "main_result" {
		t.Fatal(ph)
	}
}
