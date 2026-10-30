package store

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var refusalReasons = []string{"rule", "diff_unlanded", "non_linear", "no_merge_after_landing", "baseline_unknown", "baseline_moved", "budget_exhausted", "hard_floor", "wrong_input", "ci_red", "ci_results", "ci_missing"}

func TestStgGateAllBlockReasonsMatchProtocol(t *testing.T) {
	t.Parallel()
	for _, why := range refusalReasons {
		t.Run(why, func(t *testing.T) {
			t.Parallel()
			x := stgPolicy(t)
			m := stgMember(StgRecord(stgTestMember()))
			d := StgGateDecision{Why: why, Set: []StgMemberKey{m}, HardFloor: []StgMemberKey{m}}
			r, err := stgRefusalRecord(x.s, stgTestBatch, x.o, x.f, x.at, d)
			if err != nil {
				t.Fatal(err)
			}
			b := x.h.add(r) // Parse, Append check, Fold
			if !x.h.state().Blocks[r.String("block")].IsOpen() || b.Record.String("why") != why {
				t.Fatal("refusal missing after replay")
			}
		})
	}
}

func TestStgBlockedRecordRejectsInvalidProtocol(t *testing.T) {
	t.Parallel()
	base, detail := StgRecord(stgTestBase()), map[string]any{"members": []any{stgTestMember()}}
	originalBase, originalDetail := stgClone(base), stgClone(detail)
	for _, why := range []string{"rule", "baseline_unknown"} {
		r, err := StgBlockedRecord(stgSHA("a"), why, base, stgTestAt, "test-version", detail)
		if err != nil || r == nil {
			t.Fatal("positive control", err)
		}
	}
	if !reflect.DeepEqual(map[string]any(base), originalBase) || !reflect.DeepEqual(detail, originalDetail) {
		t.Fatal("constructor changed caller data")
	}
	if _, err := StgBlockedRecord("not-a-sha", "rule", base, stgTestAt, "test-version", detail); err == nil {
		t.Fatal("invalid P accepted")
	}
	bad := map[string]any{"ci": map[string]any{"uid": "ci", "created_at": stgTestAt.Add(time.Minute).Format(time.RFC3339Nano), "read_at": stgTestAt.Format(time.RFC3339Nano)}}
	if _, err := StgBlockedRecord(stgSHA("a"), "rule", base, stgTestAt, "test-version", bad); err == nil {
		t.Fatal("invalid CI chronology accepted")
	}
}

// Serial: real PostgreSQL Append/readback and process-wide event clock.
// Includes every controller/final-gate blocker; missing bases never relax Parse.
func TestStgBlockedRecordPGAllReasons(t *testing.T) {
	_, conn := stgPG(t)
	var at time.Time
	if err := conn.QueryRow(context.Background(), `SELECT now()`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	at = at.UTC().Truncate(time.Second)
	t.Cleanup(FreezeEventClock(at))
	for _, why := range refusalReasons {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/known=%t", why, known), func(t *testing.T) {
				base := StgRecord(stgTestBase())
				if !known {
					base = nil
				}
				version := fmt.Sprintf("%s-%t", why, known)
				r, err := StgBlockedRecord(stgSHA("a"), why, base, at, version, map[string]any{"members": []any{stgTestMember()}})
				if err != nil {
					t.Fatal(err)
				}
				a, err := stgPGAppend(conn, stgNote(r))
				if err != nil {
					t.Fatal("real Append refused constructed block", err)
				}
				s := stgPGRead(t, conn)
				b := s.Blocks[r.String("block")]
				want := why
				if !known {
					want = "baseline_unknown"
				}
				if !b.IsOpen() || b.Opened.Seq != a.Seq || b.Opened.Record.String("why") != want || !b.Opened.Record.Time("first_at").Equal(at) {
					t.Fatal("stored refusal changed", b)
				}
				if !known && why != "baseline_unknown" && b.Opened.Record.object("detail").String("refusal") != why {
					t.Fatal("original refusal lost")
				}
				if _, err = stgPGAppend(conn, stgNote(r)); err == nil {
					t.Fatal("duplicate interval accepted")
				}
				if !reflect.DeepEqual(s, stgPGRead(t, conn)) {
					t.Fatal("rejected duplicate changed interval")
				}
			})
		}
	}
}
