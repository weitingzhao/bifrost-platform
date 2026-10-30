package stgrelease

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// B5-g's exhausted-card fixture resumes at failed verification and uses this
// same controller path. Keep the protocol check executable without PostgreSQL.
func TestStgCardFailureEffectMatchesStorageProtocol(t *testing.T) {
	t.Parallel()
	c, l, runs, _ := controlFixture(t, "Failed")
	pass(t, c) // Archive the terminal run.
	pass(t, c) // Verify the failed run against its frozen inputs.
	before := l.state(t)
	runs.workloads = workloads(dig("a"), dig("c"))
	pass(t, c)
	after := l.state(t)
	effect := after.Batches[leaseBatch].Tries[1].Effect
	want, err := json.Marshal(runs.workloads)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(effect.Record["workloads"])
	if err != nil {
		t.Fatal(err)
	}
	if after.LastSeq != before.LastSeq+1 || effect.Seq != after.LastSeq || effect.Record.String("class") != "unchanged" || string(got) != string(want) || !effect.Record.Time("at").Equal(l.clock.Now()) || runs.workReads != 1 || runs.creates != 0 {
		t.Fatal("effect lost observed workloads or bypassed the controller", effect)
	}
	if !reflect.DeepEqual(before.Budget, after.Budget) || !reflect.DeepEqual(before.Debits, after.Debits) {
		t.Fatal("effect changed historical budget or backoff")
	}
	// The former B5-g fixture payload must still fail the real parser and
	// commit path; fixing a fixture must never relax the storage protocol.
	bad := store.StgRecord{}
	for key, value := range effect.Record {
		bad[key] = value
	}
	bad["workloads"] = map[string]any{}
	if _, err := store.ParseStgRelease(bad); err == nil || !strings.Contains(err.Error(), "invalid workloads") {
		t.Fatal("empty workloads accepted by parser", err)
	}
	if written, err := l.Commit(context.Background(), after.LastSeq, []store.StgRecord{bad}); written || err == nil || !strings.Contains(err.Error(), "invalid workloads") || l.state(t).LastSeq != after.LastSeq {
		t.Fatal("invalid effect changed the ledger", written, err)
	}
}
