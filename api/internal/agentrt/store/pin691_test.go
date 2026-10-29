package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// B-01: the actual host row wait, not a timing assumption, puts the exact
// claim before the capability lock while a second connection revokes it.
func Test691ClaimRevocationInterleaving(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"control", "entry revoked", "repo control", "repo narrowed"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			w := pinWorld(t)
			s := signer{t, rptest.NewKey(t)}
			if strings.HasPrefix(change, "repo") {
				ok(t, firstErr(s.sign(w, strings.Replace(pinEnvelope(2, rules.Loosen, true), "repos: [bifrost-platform]", "repos: [bifrost-platform, bifrost-trade-infra]", 1))))
			}
			p := w.pinnedTask("unit", rules.PinCoordinator)
			if strings.HasPrefix(change, "repo") {
				p.Repo = "bifrost-trade-infra"
			}
			out, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "unit", "M-1", "", []store.PinnedTask{p})
			ok(t, err)
			task := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", task) }))
			blocker, err := w.rt.Begin(ctx)
			ok(t, err)
			defer blocker.Rollback(ctx)
			_, err = blocker.Exec(ctx, `SELECT 1 FROM agentrt.host WHERE host_id=$1 FOR UPDATE`, externalHost)
			ok(t, err)
			claimant := w.db.Connect(t, w.db.Runtime)
			writer := w.db.Connect(t, w.db.Runtime)
			watch := w.db.Connect(t, w.db.Admin)
			race, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := store.ClaimPinned(race, claimant, exactReq(task, "race")); done <- e }()
			for {
				var blocked bool
				ok(t, watch.QueryRow(race, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, w.rt.PgConn().PID(), claimant.PgConn().PID()).Scan(&blocked))
				if blocked {
					break
				}
				select {
				case e := <-done:
					t.Fatalf("claim finished before host lock: %v", e)
				default:
				}
				time.Sleep(5 * time.Millisecond)
			}
			if change != "control" && change != "repo control" {
				version, keep := 2, false
				if change == "repo narrowed" {
					version, keep = 3, true
				}
				text := pinEnvelope(version, rules.Tighten, keep)
				if change == "repo narrowed" {
					_, err = store.SignRule(race, writer, store.Writer{Actor: "owner", Token: "admin"}, "standing-envelope:dev", []byte(text), s.key.Sign(t, []byte(text), rules.Namespace), s.key.Fingerprint)
				} else {
					_, err = store.TightenRule(race, writer, wr("thread:s1"), "standing-envelope:dev", []byte(text))
				}
				ok(t, err)
			}
			ok(t, blocker.Commit(ctx))
			err = <-done
			if change == "control" || change == "repo control" {
				ok(t, err)
				if w.attempts(t, task) != 1 {
					t.Fatal("control did not lease")
				}
				return
			}
			if !errors.Is(err, store.ErrNotExternal) {
				t.Fatalf("revoked authorization: %v", err)
			}
			if change == "repo narrowed" && !strings.Contains(err.Error(), "no longer permits") {
				t.Fatalf("repo refusal was masked by another check: %v", err)
			}
			if w.attempts(t, task) != 0 {
				t.Fatal("revoked claim spent an attempt")
			}
			if w.count(t, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id LIKE '%claim_pinned|race'`) != 0 {
				t.Fatal("refusal stored a key")
			}
		})
	}
}

// R-02: a single signed envelope both permits first registration and starts
// the real mission. No fixture writes remove its envelope binding.
func Test691FirstExternalEnvelopeMission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	registerSeat(t, w)
	s := signer{t, rptest.NewKey(t)}
	unknown := strings.Replace(pinEnvelope(1, rules.Initial, true), "hosts: [h1, coordinator-mbp]", "hosts: [h1, coordinator-mbp, unregistered-ordinary]", 1)
	if _, err := s.sign(w, unknown); err == nil {
		t.Fatal("unknown ordinary host accepted")
	}
	ok(t, firstErr(s.sign(w, pinEnvelope(1, rules.Initial, true))))
	out, err := store.ProposeMission(ctx, w.rt, coordinator, "mission", store.MissionProposal{Title: "handin", Goal: "unit", Success: []string{"done"}, MaxAction: "branch", MaxAttempts: 10, MaxWallHours: 1}, nil)
	ok(t, err)
	if out["state"] != "active" {
		t.Fatalf("mission not started: %v", out)
	}
	mission := out["mission_id"].(string)
	var envelope string
	ok(t, w.rt.QueryRow(ctx, `SELECT envelope_rule_id FROM agentrt.mission WHERE mission_id=$1`, mission).Scan(&envelope))
	if envelope != "standing-envelope:dev" {
		t.Fatal("mission lost its envelope")
	}
	p := w.pinnedTask("unit", rules.PinCoordinator)
	p.PlanNode = ""
	p.OffPlanReason = "one handed-in unit"
	out, err = store.ProposePinnedTasks(ctx, w.rt, coordinator, "task", mission, "", []store.PinnedTask{p})
	ok(t, err)
	task := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", task) }))
	if w.count(t, `SELECT count(*) FROM agentrt.host WHERE host_id=$1`, externalHost) != 0 {
		t.Fatal("host registered before claim")
	}
	_, err = w.exact(task, "first")
	ok(t, err)
	if w.attempts(t, task) != 1 {
		t.Fatal("first claim did not lease")
	}
	var kind string
	ok(t, w.rt.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id=$1`, externalHost).Scan(&kind))
	if kind != store.HostExternal {
		t.Fatalf("kind %s", kind)
	}
}

func Test691PersistThenSalvageImmutable(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	r := w.pinnedRun(t, "unit", time.Hour)
	a := store.Result{Report: r.report, Ref: store.AttemptRef(r.task, 1), Commit: commitB}
	facts := goodCommit(r.task)
	facts.Paths = []string{"api/internal/server/auth.go"}
	facts.Lines = 1200
	head, read := forge(a.Commit, facts)
	_, err := store.PersistChecked(ctx, w.rt, r.fence, a, head, read)
	ok(t, err)
	_, err = w.rt.Exec(ctx, `UPDATE agentrt.attempt SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE attempt_id=$1`, r.attempt)
	ok(t, err)
	_, _, err = store.SweepLeases(ctx, w.rt)
	ok(t, err)
	before := w.count(t, `SELECT count(*) FROM agentrt.event`)
	for _, variant := range []string{"commit", "report", "attachments"} {
		b := a
		switch variant {
		case "commit":
			b.Commit = strings.Repeat("c", 40)
		case "report":
			b.Report = ""
		case "attachments":
			b.Artifacts = []string{r.report}
		}
		h, cr := forge(b.Commit, goodCommit(r.task))
		_, err = store.SalvageChecked(ctx, w.rt, r.attempt, externalHost, b, h, cr)
		if !errors.Is(err, store.ErrState) {
			t.Fatalf("%s: %v", variant, err)
		}
		if w.count(t, `SELECT count(*) FROM agentrt.event`) != before {
			t.Fatal("replacement wrote an event")
		}
	}
	_, err = store.SalvageChecked(ctx, w.rt, r.attempt, externalHost, a, head, read)
	ok(t, err)
	var stored string
	ok(t, w.rt.QueryRow(ctx, `SELECT pushed_commit FROM agentrt.attempt WHERE attempt_id=$1`, r.attempt).Scan(&stored))
	if stored != a.Commit {
		t.Fatal("persisted identity changed")
	}
}

func Test691FirstRegistrationRevocationInterleaving(t *testing.T) {
	t.Parallel()
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "control", true: "revoked"}[revoke], func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			registerSeat(t, w)
			s := signer{t, rptest.NewKey(t)}
			env := strings.Replace(pinEnvelope(1, rules.Initial, true), "hosts: [h1, coordinator-mbp]", "hosts: [h1]", 1)
			ok(t, firstErr(s.sign(w, env)))
			task := w.readyPinned(t, "unit")
			blocker, err := w.rt.Begin(ctx)
			ok(t, err)
			defer blocker.Rollback(ctx)
			_, err = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(742112,hashtext($1))`, externalHost)
			ok(t, err)
			claimant := w.db.Connect(t, w.db.Runtime)
			writer := w.db.Connect(t, w.db.Runtime)
			watch := w.db.Connect(t, w.db.Admin)
			race, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := store.ClaimPinned(race, claimant, exactReq(task, "first")); done <- e }()
			for {
				var blocked bool
				ok(t, watch.QueryRow(race, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, w.rt.PgConn().PID(), claimant.PgConn().PID()).Scan(&blocked))
				if blocked {
					break
				}
				select {
				case e := <-done:
					t.Fatalf("registration finished early: %v", e)
				default:
				}
				time.Sleep(5 * time.Millisecond)
			}
			if revoke {
				revoked := strings.Replace(pinEnvelope(2, rules.Tighten, false), "hosts: [h1, coordinator-mbp]", "hosts: [h1]", 1)
				_, err = store.TightenRule(race, writer, wr("thread:s1"), "standing-envelope:dev", []byte(revoked))
				ok(t, err)
			}
			ok(t, blocker.Commit(ctx))
			err = <-done
			if !revoke {
				ok(t, err)
				return
			}
			if !errors.Is(err, store.ErrNotExternal) {
				t.Fatalf("revoked registration: %v", err)
			}
			if w.count(t, `SELECT count(*) FROM agentrt.host WHERE host_id=$1`, externalHost) != 0 || w.attempts(t, task) != 0 {
				t.Fatal("revoked first claim registered or leased")
			}
		})
	}
}
