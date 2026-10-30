package store_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Database now is the sole anchor: mission.created_at is PostgreSQL-owned.
// FreezeEventClock cannot move that clock or repair a fence after creation.
func b5fWorld(t *testing.T, mode string) (*world, time.Time) {
	t.Helper()
	w := newKernelWorld(t)
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT now()`).Scan(&at))
	at = at.UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at.Add(-time.Hour)))
	body := landingRuleBody
	if mode == "none" {
		body = strings.Replace(body, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)
	}
	expires := until
	if mode == "missing" {
		expires = at.Add(time.Hour).Format(time.RFC3339)
	}
	_, err := (signer{t, rptest.NewKey(t)}).sign(w, ruleText("landing:dev", 1, "initial", since, expires, body))
	ok(t, err)
	_, err = store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.ObserveLandingEnabled(ctx, tx, "prod", "dev", "bifrost-platform", store.DecisionAt())
		return err
	}))
	store.FreezeEventClock(at)
	w.goalMission(t, "M-3")
	return w, at
}

func b5fEvidence(t *testing.T, w *world, at time.Time, id string) {
	t.Helper()
	store.FreezeEventClock(at)
	d := "sha256:" + strings.Repeat("d", 64)
	b5Append(t, w, b5Record("evidence", "ext", "evidence", id, "source", "external", "run", "manual", "P", commitB, "U", commitA, "infra", commitA,
		"content", map[string]any{"api_digest": d, "console_digest": d}, "deployed", map[string]any{"pin_sync_ok": true, "workloads": map[string]any{"observed": true}, "at": at.Format(time.RFC3339Nano)}, "health", map[string]any{"status": "ok"}))
}

func b5fCover(t *testing.T, w *world, m store.StgMemberKey, id string, at time.Time) {
	t.Helper()
	store.FreezeEventClock(at)
	s, err := store.ReadStgRelease(ctx, w.rt)
	ok(t, err)
	written, err := store.CommitStgCoverage(ctx, w.rt, "dev", s.LastSeq, id, m, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective", ReadAt: at})
	ok(t, err)
	if !written {
		t.Fatal("coverage did not commit")
	}
}

// Use the production gate to construct, validate and persist each refusal.
// Repeating it must preserve the interval and never issue a send permit.
func b5fRefuse(t *testing.T, x *b5Fixture, why string) {
	t.Helper()
	for i := 0; i < 2; i++ {
		before := x.read(t)
		p, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
		ok(t, err)
		after := x.read(t)
		if d.Allowed || p.IntentSeq != 0 || d.Why != why || d.Block == "" || !after.Blocks[d.Block].IsOpen() || after.Blocks[d.Block].Opened.Record.String("why") != why || len(after.Blocks) != 1 || len(after.Budget) != 0 {
			t.Fatal("refusal did not persist without a send or debit", p, d, after.Blocks, after.Budget)
		}
		if i > 0 && !reflect.DeepEqual(before, after) {
			t.Fatal("repeated refusal changed its interval or ledger")
		}
	}
}

// Serial: PostgreSQL fixtures and FreezeEventClock; same close入口 in all modes.
func TestStgB4PGNoneEnabledMissingAndClosed(t *testing.T) {
	for _, mode := range []string{"none", "enabled", "missing"} {
		t.Run(mode, func(t *testing.T) {
			w, at := b5fWorld(t, mode)
			root := s66Landed(t, w, "M-3", "bifrost-platform", at)
			decision := at.Add(2 * time.Minute)
			if mode == "missing" {
				decision = at.Add(2 * time.Hour)
			}
			if st := closePass(t, w, decision); st != "active" {
				t.Fatal("closed before requirements", st)
			}
			view, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
			ok(t, err)
			want := map[string]string{"none": "evidence", "enabled": "stg_pending", "missing": "rule"}[mode]
			if len(view.Roots) != 1 || view.Roots[0].Reason != want {
				t.Fatal("wrong policy class", view.Roots)
			}
			if (view.Roots[0].STG != nil) != (mode != "none") {
				t.Fatal("none changed the legacy status shape", view.Roots)
			}
			if mode == "missing" {
				return
			}
			if mode == "enabled" {
				b5fEvidence(t, w, at.Add(3*time.Minute), "cluster/cicd/manual")
				b5fCover(t, w, store.StgMemberKey{Root: root, Op: "land:" + root + ":1", R: commitB}, "cluster/cicd/manual", at.Add(4*time.Minute))
				if st := closePass(t, w, at.Add(5*time.Minute)); st != "active" {
					t.Fatal("historical P read counted as current main", st)
				}
			}
			effectiveRead(t, w, at.Add(6*time.Minute))
			if st := closePass(t, w, at.Add(7*time.Minute)); st != "done" {
				t.Fatal("fresh delivery failed", st)
			}
			before, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
			ok(t, err)
			if before.Closed == nil || before.Closed.Delivered != 1 {
				t.Fatal("no durable close", before)
			}
			// Switch none -> enabled and add external facts after closure.
			if mode == "none" {
				_, err = (signer{t, rptest.NewKey(t)}).sign(w, ruleText("landing:dev", 2, "loosen", since, until, landingRuleBody))
				ok(t, err)
			}
			b5fEvidence(t, w, at.Add(8*time.Minute), "cluster/cicd/late")
			if st := closePass(t, w, at.Add(9*time.Minute)); st != "done" {
				t.Fatal("closed mission reopened")
			}
			after, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
			ok(t, err)
			if !reflect.DeepEqual(before.Closed, after.Closed) {
				t.Fatal("late release rewrote close classification")
			}
		})
	}
}

// Serial: the round and both per-root main reads are real stored events.
func TestStgB4PGPartialEvidenceKeepsRoundAndNewProofChangesIt(t *testing.T) {
	w, at := b5fWorld(t, "enabled")
	root1 := s66Landed(t, w, "M-3", "bifrost-platform", at)
	root2 := s66Landed(t, w, "M-3", "bifrost-platform", at)
	id := "cluster/cicd/manual"
	// Two contradictory deployment observations at the same database-based
	// time leave the current baseline unknown, while historical covers stand.
	store.FreezeEventClock(at.Add(time.Minute))
	x := b5Snapshot(t, w, root1, "land:"+root1+":1", store.DecisionAt())
	b5fEvidence(t, w, at.Add(time.Minute), id)
	for _, root := range []string{root1, root2} {
		b5fCover(t, w, store.StgMemberKey{Root: root, Op: "land:" + root + ":1", R: commitB}, id, at.Add(2*time.Minute))
	}
	closePass(t, w, at.Add(3*time.Minute))
	if closeRounds(t, w) != 1 {
		t.Fatal("missing round")
	}
	readOne(t, w, "land:"+root1+":1", at.Add(4*time.Minute), "effective")
	closePass(t, w, at.Add(5*time.Minute))
	if closeRounds(t, w) != 1 {
		t.Fatal("first root invalidated the other root's round")
	}
	// A new blocking interval changes the stable snapshot; diagnostics and
	// budget-only changes are covered by the no-I/O replay test.
	x.advance(at.Add(6 * time.Minute))
	b5fRefuse(t, x, "baseline_unknown")
	closePass(t, w, at.Add(7*time.Minute))
	if closeRounds(t, w) != 2 {
		t.Fatal("block did not change snapshot")
	}
	readOne(t, w, "land:"+root2+":1", at.Add(8*time.Minute), "effective")
	if st := closePass(t, w, at.Add(9*time.Minute)); st != "active" || closeRounds(t, w) != 2 {
		t.Fatal("old root evidence carried into new snapshot", st)
	}
	readOne(t, w, "land:"+root1+":1", at.Add(10*time.Minute), "effective")
	if st := closePass(t, w, at.Add(11*time.Minute)); st != "done" || closeRounds(t, w) != 2 {
		t.Fatal("fresh roots did not close", st)
	}
}

// Serial: real dual connections contend for the CAS, with no cluster I/O.
func TestStgCoveragePGCASAndNoneFence(t *testing.T) {
	for _, none := range []bool{false, true} {
		t.Run(fmt.Sprint(none), func(t *testing.T) {
			w, at := b5fWorld(t, "enabled")
			root := s66Landed(t, w, "M-3", "bifrost-platform", at)
			id := "cluster/cicd/manual"
			b5fEvidence(t, w, at.Add(time.Minute), id)
			if none {
				_, err := store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: none", 1))))
				ok(t, err)
			}
			s, err := store.ReadStgRelease(ctx, w.rt)
			ok(t, err)
			second := w.db.Connect(t, w.db.Runtime)
			type result struct {
				written bool
				err     error
			}
			results := make(chan result, 2)
			for _, db := range []store.DB{w.rt, second} {
				safego.Go("test-stg-cover-cas", func() {
					did, err := store.CommitStgCoverage(ctx, db, "dev", s.LastSeq, id, store.StgMemberKey{Root: root, Op: "land:" + root + ":1", R: commitB}, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective", ReadAt: store.DecisionAt()})
					results <- result{did, err}
				})
			}
			count := 0
			for range 2 {
				select {
				case r := <-results:
					ok(t, r.err)
					if r.written {
						count++
					}
				case <-time.After(15 * time.Second):
					t.Fatal("coverage writers deadlocked")
				}
			}
			after, err := store.ReadStgRelease(ctx, w.rt)
			ok(t, err)
			want := 1
			if none {
				want = 0
			}
			if count != want || len(after.Covers) != want || after.LastSeq != s.LastSeq+int64(2*want) {
				t.Fatal("non-atomic or disabled coverage", count, after.LastSeq, s.LastSeq)
			}
		})
	}
}

// Serial: existing B3 Owner count, B5 coverage, and B4 close use real rows.
func TestStgB4PGInMainNeedsOwnerAndRecordedHCoverage(t *testing.T) {
	for _, choice := range []string{"count_as_delivered", "confirm_not_delivered"} {
		t.Run(choice, func(t *testing.T) {
			w, at := b5fWorld(t, "enabled")
			root := s66Root(t, w, "M-3", "bifrost-platform")
			op := "land:" + root + ":1"
			ok(t, appendNote(w, landingRepoStream, map[string]any{"landing": newLandingRecord(op, "failed", "why", "in_main"), "text": "the landing loop will not claim this task: in_main (attempt x, A " + commitA + ", base " + commitA + ", mission active; main history effective at commit " + commitB + ", read at " + commitB + ")"}))
			b5fEvidence(t, w, at.Add(time.Minute), "cluster/cicd/manual")
			if st := closePass(t, w, at.Add(2*time.Minute)); st != "active" {
				t.Fatal("release without Owner closed in_main", st)
			}
			f := newFakeCards(func() time.Time { return store.DecisionAt() })
			cardPass(t, w, f, at.Add(3*time.Minute))
			var card store.CardRecord
			for _, c := range cardsNow(t, w) {
				if c.Op == op {
					card = c
				}
			}
			answer := ""
			for key, value := range card.Options {
				if value == choice {
					answer = key
				}
			}
			if answer == "" {
				t.Fatal("Owner choice missing", card)
			}
			f.answer(card.QuestionID, answer, at.Add(4*time.Minute))
			cardPass(t, w, f, at.Add(5*time.Minute))
			want := "active"
			if choice == "confirm_not_delivered" {
				want = "stopped"
			}
			if st := closePass(t, w, at.Add(6*time.Minute)); st != want {
				t.Fatal("Owner count ignored O-3", st, want)
			}
			if choice == "confirm_not_delivered" {
				return
			}
			s, err := store.ReadStgRelease(ctx, w.rt)
			ok(t, err)
			candidates := store.StgCoverageCandidates(s)
			if len(candidates) != 1 || candidates[0].R != commitB {
				t.Fatal("used A instead of recorded H", candidates)
			}
			store.FreezeEventClock(at.Add(7 * time.Minute))
			did, err := store.CommitStgCoverage(ctx, w.rt, "dev", s.LastSeq, "cluster/cicd/manual", candidates[0], store.MainEvidence{Head: commitB, HasR: true, Validity: "unknown", ReadAt: store.DecisionAt()})
			ok(t, err)
			if !did {
				t.Fatal("H containment did not cover")
			}
			if st := closePass(t, w, at.Add(8*time.Minute)); st != "done" {
				t.Fatal("O-3 positive did not close", st)
			}
		})
	}
}

// Serial: the legacy card disposition and new hard-floor withholding coexist
// in the real stream; the same B4 entry must retain their priority.
func TestStgB4PGLegacyOwnerAndWithheldCoexist(t *testing.T) {
	for _, oldOwner := range []bool{false, true} {
		t.Run(fmt.Sprint(oldOwner), func(t *testing.T) {
			w, at := b5fWorld(t, "enabled")
			root := s66Landed(t, w, "M-3", "bifrost-platform", at)
			op := "land:" + root + ":1"
			if oldOwner {
				readOne(t, w, op, at.Add(time.Minute), "unknown")
				f := newFakeCards(func() time.Time { return store.DecisionAt() })
				cardPass(t, w, f, at.Add(3*time.Hour))
				var question, answer string
				for _, c := range cardsNow(t, w) {
					if c.Op == op {
						for k, v := range c.Options {
							if v == "confirm_not_delivered" {
								question, answer = c.QuestionID, k
							}
						}
					}
				}
				if answer == "" {
					t.Fatal("legacy disposition card missing")
				}
				f.answer(question, answer, at.Add(3*time.Hour+time.Minute))
				cardPass(t, w, f, at.Add(3*time.Hour+2*time.Minute))
			}
			store.FreezeEventClock(at.Add(4 * time.Hour))
			x := b5Snapshot(t, w, root, op, store.DecisionAt())
			x.obs.Commits[0].Changes[0].OldPath = ".github/workflows/ci.yaml"
			x.obs.Commits[0].Changes[0].NewPath = ".github/workflows/ci.yaml"
			b5fRefuse(t, x, "hard_floor")
			view, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
			ok(t, err)
			want := "stg_withheld"
			if oldOwner {
				want = "owner"
			}
			if len(view.Roots) != 1 || view.Roots[0].Class != "disposed" || view.Roots[0].Reason != want || view.Roots[0].STG.Phase != "withheld" {
				t.Fatal("legacy/new priority", view.Roots)
			}
			if st := closePass(t, w, at.Add(4*time.Hour+time.Minute)); st != "stopped" {
				t.Fatal("disposition did not settle", st)
			}
		})
	}
}

// Serial: PATH is a test-only Git transport shim; real Git reads fixed P.
func TestStgReleaseHistoricalGitRead(t *testing.T) {
	f := localGit704(t)
	r := f.result(platformRepo, "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", true)
	f.run(f.sources[platformRepo], "revert", "--no-edit", r)
	reverted := f.run(f.sources[platformRepo], "rev-parse", "HEAD")
	f.run(f.sources[platformRepo], "push", f.remotes[platformRepo], "HEAD:refs/heads/main")
	g, err := landinggit.LanderFromEnv("prod", platformRepo)
	ok(t, err)
	for _, p := range []string{r, reverted} {
		head, has, hist, err := g.ReleaseEvidence(ctx, p, r, "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", r)
		ok(t, err)
		want := "effective"
		if p == reverted {
			want = "reverted"
		}
		if head != p || !has || string(hist.State) != want {
			t.Fatal("historical P replaced by current main", head, has, hist)
		}
	}
	_, has, _, err := g.ReleaseEvidence(ctx, f.bases[platformRepo], r, "", "")
	ok(t, err)
	if has {
		t.Fatal("H not in release P accepted")
	}
}
