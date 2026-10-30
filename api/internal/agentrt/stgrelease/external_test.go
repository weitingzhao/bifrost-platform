package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type externalRuns struct {
	*controlRuns
	listed  []*unstructured.Unstructured
	byUID   map[string]delivery.STGRunObservation
	lists   int
	listErr error
}

func (r *externalRuns) ListSucceededSTG(context.Context) ([]*unstructured.Unstructured, error) {
	r.lists++
	return r.listed, r.listErr
}
func (r *externalRuns) ObserveRun(_ context.Context, id delivery.STGRunIdentity) (delivery.STGRunObservation, error) {
	r.observations++
	if r.observeErr != nil {
		return delivery.STGRunObservation{}, r.observeErr
	}
	o, ok := r.byUID[string(id.UID)]
	if !ok {
		return delivery.STGRunObservation{}, errors.New("missing test run")
	}
	return o, nil
}
func (l *memoryLedger) Cover(_ context.Context, seq int64, id string, m store.StgMemberKey, ev store.MainEvidence) (bool, error) {
	l.commits++
	if seq != int64(len(l.entries)) {
		return false, nil
	}
	s, err := store.StgFold(l.entries)
	if err != nil {
		return false, err
	}
	events, err := store.StgCoverageEvents(s, id, m, ev, l.clock.Now())
	if err != nil {
		return false, err
	}
	pending := append([]store.StgEntry{}, l.entries...)
	for _, e := range events {
		pending = append(pending, store.StgEntry{Seq: int64(len(pending) + 1), At: l.clock.Now(), Event: e})
	}
	if _, err := store.StgFold(pending); err != nil {
		return false, err
	}
	l.entries = pending
	return true, nil
}

func externalFixture(t *testing.T) (*Controller, *memoryLedger, *externalRuns, *leaseWindows) {
	t.Helper()
	c, l, r, w := controlFixture(t, "Succeeded")
	l.entries = nil
	o := controlObservationUID(t, delivery.NamedDeliverInput{Batch: 2, Try: 1, IntentSeq: 30, P: sha("a"), U: sha("b"), Infra: sha("c")}, "Succeeded", "manual-uid")
	o.Run.SetAnnotations(nil)
	x := &externalRuns{controlRuns: r, listed: []*unstructured.Unstructured{o.Run}, byUID: map[string]delivery.STGRunObservation{"manual-uid": o}}
	c.Runs = x
	c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, m store.StgMemberKey, p string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
	}
	return c, l, x, w
}

func externalLanded(t *testing.T, l *memoryLedger, m store.StgMemberKey) {
	t.Helper()
	ref := "refs/heads/land/prod/" + m.Root + "/1"
	now := l.clock.Now().Format(time.RFC3339Nano)
	record := func(kind string, kv ...any) map[string]any {
		r := map[string]any{"v": 1, "op": m.Op, "kind": kind}
		for i := 0; i < len(kv); i += 2 {
			r[kv[i].(string)] = kv[i+1]
		}
		return r
	}
	for _, r := range []map[string]any{
		record("claimed", "task", m.Root, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "A", m.R, "rule_version", 1),
		record("prepared", "R", m.R, "M0", sha("e"), "committer_date", now, "dry_run", false),
		record("branch_intent", "ref", ref, "R", m.R), record("branch_pushed", "ref", ref, "R", m.R),
		record("ci_seen", "run", "ci1", "status", "Succeeded", "R", m.R, "created_at", now),
		record("main_intent", "R", m.R, "M0", sha("e")), record("main_result", "outcome", "ok"), record("landed", "R", m.R),
	} {
		if r["kind"] == "ci_seen" {
			r["created_at"] = l.clock.Now().Format(time.RFC3339Nano)
		}
		l.entries = append(l.entries, store.StgEntry{Seq: int64(len(l.entries) + 1), At: l.clock.Now(), Event: store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"landing": r}}})
		l.clock.Step(time.Second)
	}
	_ = l.state(t)
}

func externalPass(t *testing.T, c *Controller) {
	t.Helper()
	if err := c.ExternalPass(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExternalSameRunBackfillsLaterMembers(t *testing.T) {
	t.Parallel()
	c, l, r, w := externalFixture(t)
	externalLanded(t, l, controlMember)
	externalPass(t, c)
	s := l.state(t)
	if len(s.Evidence) != 1 || len(s.Covers) != 1 {
		t.Fatal("initial settlement", len(s.Evidence), len(s.Covers))
	}
	id := "cluster/cicd/manual-uid"
	if s.Evidence[id].Record.String("source") != "external" {
		t.Fatal("external provenance")
	}
	seq := s.Evidence[id].Seq
	second := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("2")}
	externalLanded(t, l, second)
	externalPass(t, c)
	s = l.state(t)
	if len(s.Evidence) != 1 || len(s.Covers) != 2 || s.Evidence[id].Seq != seq {
		t.Fatal("same run did not backfill", len(s.Evidence), len(s.Covers))
	}
	// Retention removed the run. Its durable identity still accepts covers.
	r.listed = nil
	third := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAX", Op: "land:three:1", R: sha("3")}
	externalLanded(t, l, third)
	externalPass(t, c)
	s = l.state(t)
	if len(s.Covers) != 3 || s.Evidence[id].Seq != seq {
		t.Fatal("retention lost coverage", len(s.Covers))
	}
	last := s.LastSeq
	for range 3 {
		externalPass(t, c)
	}
	if l.state(t).LastSeq != last || r.creates != 0 || len(s.Budget) != 0 || windowCalls(w) != 0 {
		t.Fatal("duplicate facts, automatic spend, Create or window")
	}
}

func TestExternalAutomaticBeforeAndAfterBinding(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"before-bound", "before-auto", "after-auto"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			c, l, r, w := controlFixture(t, "Succeeded")
			x := &externalRuns{controlRuns: r, listed: []*unstructured.Unstructured{r.obs.Run}, byUID: map[string]delivery.STGRunObservation{"run-uid": r.obs}}
			c.Runs = x
			if order == "before-bound" {
				l.entries = l.entries[:len(l.entries)-1]
			}
			if order == "after-auto" {
				pass(t, c)
				pass(t, c)
			}
			before := l.state(t).LastSeq
			externalPass(t, c)
			s := l.state(t)
			if s.LastSeq != before {
				t.Fatal("external pass adopted automatic run")
			}
			for _, e := range s.Evidence {
				if e.Record.String("run") == r.obs.Identity.Name && e.Record.String("source") != "automatic" {
					t.Fatal("automatic relabelled external")
				}
			}
			if order == "before-auto" {
				pass(t, c)
				pass(t, c)
				if l.state(t).Evidence["cluster/cicd/run-uid"].Record.String("source") != "automatic" {
					t.Fatal("automatic observer did not complete")
				}
			}
			if order == "before-bound" && windowCalls(w) != 0 {
				t.Fatal("external took window")
			}
		})
	}
}

func TestExternalSameDigestsNeedEachRunsDeploymentProof(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := externalFixture(t)
			other := controlObservationUID(t, delivery.NamedDeliverInput{Batch: 3, Try: 1, IntentSeq: 40, P: sha("2"), U: sha("b"), Infra: sha("c")}, "Succeeded", "second-uid")
			other.Run.SetAnnotations(nil)
			if !valid {
				delete(other.Children, "gitops-sync")
			}
			r.listed = append(r.listed, other.Run)
			r.byUID["second-uid"] = other
			err := c.ExternalPass(context.Background())
			if (err == nil) != valid {
				t.Fatal("deployment proof acceptance", err)
			}
			s := l.state(t)
			if s.Evidence["cluster/cicd/manual-uid"].Seq == 0 || (s.Evidence["cluster/cicd/second-uid"].Seq > 0) != valid {
				t.Fatal("equal digest borrowed deployment evidence", s.Evidence)
			}
		})
	}
}

func TestExternalRefusalsHaveSameEntrySuccess(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "unreadable-run", "workloads", "pin", "health", "unknown-health", "wrong-head", "ineffective", "absent", "git-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := externalFixture(t)
			externalLanded(t, l, controlMember)
			switch mode {
			case "unreadable-run":
				r.observeErr = errors.New("read")
			case "workloads":
				r.workloads = workloads(dig("a"), dig("c"))
			case "pin":
				r.pinErr = delivery.ErrSTGEvidence
			case "health":
				r.health.Outcome = "failed"
			case "unknown-health":
				r.health.Outcome = "unknown"
			case "wrong-head", "ineffective", "absent", "git-error":
				c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
					e := store.MainEvidence{Head: p, HasR: true, Validity: "effective"}
					switch mode {
					case "wrong-head":
						e.Head = sha("f")
					case "ineffective":
						e.Validity = "reverted"
					case "absent":
						e.HasR = false
					case "git-error":
						return e, errors.New("git read")
					}
					return e, nil
				}
			}
			_ = c.ExternalPass(context.Background())
			if (len(l.state(t).Covers) == 1) != (mode == "success") {
				t.Fatal("coverage accepted wrong proof", mode)
			}
			if r.creates != 0 {
				t.Fatal("external created run")
			}
		})
	}
}

func TestExternalNoneAndLiveOff(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"enabled", "none", "live-off", "changed-none"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, w := externalFixture(t)
			externalLanded(t, l, controlMember)
			p := c.Policy()
			rule := *p.Rule
			land := *rule.Landing
			land.Repos = append(land.Repos[:0:0], land.Repos...)
			rule.Landing = &land
			p.Rule = &rule
			if mode == "none" {
				land.Repos[0].STGRelease = "none"
			}
			if mode == "live-off" {
				p.Live = false
			}
			c.Policy = func() WindowPolicy { return p }
			if mode == "changed-none" {
				c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
					land.Repos[0].STGRelease = "none"
					return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
				}
			}
			l.reads, l.commits = 0, 0
			externalPass(t, c)
			if mode == "none" {
				if l.reads != 0 || l.commits != 0 || r.lists != 0 {
					t.Fatal("none performed I/O")
				}
			} else if mode == "changed-none" {
				if len(l.state(t).Covers) != 0 {
					t.Fatal("none change during read wrote coverage")
				}
			} else if len(l.state(t).Covers) != 1 {
				t.Fatal("enabled observation held by live")
			}
			if windowCalls(w) != 0 || r.creates != 0 {
				t.Fatal("external acquired execution rights")
			}
		})
	}
}

func TestExternalCoveragePreservesExecutionSlot(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		t.Run(fmt.Sprint(occupied), func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Running")
			if !occupied {
				l.entries = l.entries[:len(l.entries)-2]
			}
			externalLanded(t, l, controlMember)
			o := controlObservationUID(t, delivery.NamedDeliverInput{Batch: 2, Try: 1, IntentSeq: 30, P: sha("a"), U: sha("b"), Infra: sha("c")}, "Succeeded", "manual-uid")
			o.Run.SetAnnotations(nil)
			c.Runs = &externalRuns{controlRuns: r, listed: []*unstructured.Unstructured{o.Run}, byUID: map[string]delivery.STGRunObservation{"manual-uid": o}}
			c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
				return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
			}
			externalPass(t, c)
			// Observation records coverage; the shared batch reconciler owns
			// settlement and must retain every outstanding execution.
			if l.state(t).Batches[leaseBatch].Phase == "CLOSED" {
				t.Fatal("external observation settled the batch")
			}
			if !occupied {
				pass(t, c)
			}
			s := l.state(t)
			b := s.Batches[leaseBatch]
			if len(s.Covers) != 1 || (b.Phase == "CLOSED") == occupied || b.Tries[b.Try].Occupied() != occupied {
				t.Fatal("coverage changed outstanding execution", b.Phase)
			}
			if occupied && s.Members[controlMember].To != "" {
				t.Fatal("running member settled")
			}
			if !occupied && s.Members[controlMember].To != "covered" {
				t.Fatal("idle member not settled")
			}
		})
	}
}

func windowCalls(w *leaseWindows) int {
	_, counts := w.snapshot()
	return counts[0] + counts[1] + counts[2] + counts[3]
}
