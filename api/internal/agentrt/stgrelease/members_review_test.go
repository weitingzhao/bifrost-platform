package stgrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func reviewLandingRecord(t *testing.T, l *memoryLedger, m store.StgMemberKey, kind string, kv ...any) {
	t.Helper()
	r := map[string]any{"v": landing.Version, "kind": kind, "op": m.Op}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	l.entries = append(l.entries, store.StgEntry{Seq: int64(len(l.entries) + 1), At: l.clock.Now(), Event: store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", TaskID: m.Root, Body: map[string]any{"landing": r}}})
	_ = l.state(t)
}

func reviewLanded(t *testing.T, l *memoryLedger, m store.StgMemberKey) {
	t.Helper()
	ref := "refs/heads/land/prod/" + m.Root + "/1"
	reviewLandingRecord(t, l, m, "claimed", "task", m.Root, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "A", m.R, "rule_version", 1)
	reviewLandingRecord(t, l, m, "prepared", "R", m.R, "M0", sha("e"), "committer_date", l.clock.Now().Format(time.RFC3339Nano), "dry_run", false)
	reviewLandingRecord(t, l, m, "branch_intent", "ref", ref, "R", m.R)
	reviewLandingRecord(t, l, m, "branch_pushed", "ref", ref, "R", m.R)
	l.clock.Step(time.Second)
	reviewLandingRecord(t, l, m, "ci_seen", "run", "landing-ci", "status", "Succeeded", "R", m.R, "created_at", l.clock.Now().Format(time.RFC3339Nano))
	reviewLandingRecord(t, l, m, "main_intent", "R", m.R, "M0", sha("e"))
	reviewLandingRecord(t, l, m, "main_result", "outcome", "ok")
	reviewLandingRecord(t, l, m, "landed", "R", m.R)
}

func TestControlOpenMemberValidityClassification(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"effective", "manual_revert", "landed_revert", "absent", "unknown", "read_failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			accepted := object(l.state(t).Batches[leaseBatch].CI["ci-uid"].Record, "ci")
			accepted["platform_commit"] = sha("9")
			l.entries = l.entries[:2] // only baseline and OPEN; reversal follows admission.
			reviewLanded(t, l, controlMember)
			reversalMember := reviewOther
			reversalMember.R = sha("9")
			if mode == "landed_revert" {
				reviewLanded(t, l, reversalMember)
			}
			if mode == "manual_revert" || mode == "landed_revert" {
				reviewLandingRecord(t, l, controlMember, "reverted", "R", controlMember.R, "by", sha("9"))
			}
			if mode != "read_failure" {
				l.effective(t, controlMember, sha("9"))
				proof := l.entries[len(l.entries)-1].Event.Body["landing_main"].(map[string]any)
				switch mode {
				case "manual_revert", "landed_revert":
					proof["validity"], proof["reverted_by"] = "reverted", sha("9")
				case "absent":
					proof["validity"], proof["has_r"] = "absent", false
				case "unknown":
					proof["validity"] = "unknown"
				}
			}
			l.add(t, batchRecord("p_selected", leaseBatch, 0, "P", sha("9"), "at", l.clock.Now().Format(time.RFC3339Nano)), batchRecord("ci_accepted", leaseBatch, 0, "P", sha("9"), "ci", map[string]any(accepted)))
			ranges, reads := 0, 0
			readErr := errors.New("git read failed")
			c.Sources = Sources{
				Head: func(context.Context) (string, error) { return sha("9"), nil },
				CI: func(context.Context, string) (store.StgCIObservation, error) {
					return store.StgCIObservation{Status: "Succeeded", UID: "ci-uid", ReadAt: accepted.Time("read_at"), Accepted: map[string]any(accepted)}, nil
				},
				Range: func(_ context.Context, base, p string) ([]landing.ReleaseCommit, error) {
					ranges++
					if base != sha("e") || p != sha("9") {
						t.Fatal("range authorization was shortened")
					}
					last := "Manual revert"
					if mode == "landed_revert" {
						last = "Change\n\nLanded-By: agentrt-prod/" + reversalMember.Op + "\n"
					}
					return []landing.ReleaseCommit{{SHA: controlMember.R, Parents: []string{base}, Message: "Change\n\nLanded-By: agentrt-prod/" + controlMember.Op + "\n"}, {SHA: p, Parents: []string{controlMember.R}, Message: last}}, nil
				},
				MainEvidence: func(context.Context, string, string, string, string) (store.MainEvidence, error) {
					reads++
					return store.MainEvidence{}, readErr
				},
			}
			// A non-nil DB lets the genuine source-error path run; no SQL is
			// reachable because the source returns an error before persistence.
			c.DB = struct{ store.DB }{}
			for range 5 {
				err := c.prepare(context.Background(), l.state(t), leaseBatch, l.state(t).Batches[leaseBatch])
				if mode == "unknown" || mode == "read_failure" {
					if !errors.Is(err, readErr) {
						t.Fatal("reader failure was lost", err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				break
			}
			s := l.state(t)
			switch mode {
			case "unknown", "read_failure":
				if ranges != 0 || reads != 5 || s.Batches[leaseBatch].Phase != "OPEN" || len(s.Blocks) != 0 {
					t.Fatal("unknown read authorized or settled the batch")
				}
			case "landed_revert":
				if ranges != 1 || s.Members[controlMember].To != "disposed" || s.Batches[leaseBatch].Phase != "CLOSED" {
					t.Fatal("legal later landing did not hand off the original batch")
				}
			default:
				if ranges != 1 || s.Batches[leaseBatch].Phase != "SNAP" || s.Members[controlMember].To != "" {
					t.Fatal("definite proof did not reach full-range snapshot for final authorization", mode)
				}
			}
			if r.creates != 0 {
				t.Fatal("member preparation bypassed final gate")
			}
		})
	}
}
