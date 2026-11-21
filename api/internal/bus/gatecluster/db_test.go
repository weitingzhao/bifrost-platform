package gatecluster_test

import (
	"context"
	"regexp"
	"sort"
	"testing"

	agentactor "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the actor_registered vocabulary and the seed, in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// 2026-11-16: bus step 2a (migration 0026, one PROD migration for the
// register): every type of registry/rocket.yaml is taken by
// event_type_closed, and a type outside it is still refused.
func TestTheDatabaseTakesEveryRegisteredType(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	conn := db.Connect(t, db.Admin)
	var def string
	if err := conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TEMP TABLE vocab (event_type text, CONSTRAINT event_type_closed `+def+`)`); err != nil {
		t.Fatal(err)
	}
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for typ := range all["rocket"].Channels {
		types = append(types, typ)
	}
	sort.Strings(types)
	// Since 0041 (2026-11-20) the database allows bus topics by their shape and
	// the register is their closed set: no count of types is written here.
	if len(types) < 29 {
		t.Fatalf("the register has %d types: it lost some", len(types))
	}
	for _, typ := range types {
		if _, err := conn.Exec(ctx, `INSERT INTO vocab VALUES ($1)`, typ); err != nil {
			t.Errorf("%s is refused: %v", typ, err)
		}
	}
	// The negative controls: the database refuses a name of neither shape, and
	// an unregistered topic of the right shape is refused by the ledger's
	// vocabulary (Append asks it before the database), not by the database.
	if _, err := conn.Exec(ctx, `INSERT INTO vocab VALUES ('Decision Recorded')`); err == nil {
		t.Error("a name of no allowed shape is taken")
	}
	if eventlog.IsEventType("decision.entry.recorded") {
		t.Error("an unregistered topic is in the ledger's vocabulary")
	}
}

// 2026-11-16: bus step 2a (RULES.md §7): the event log takes gate-cluster's
// events from its registered actor, a finish is written once per run, and
// each run is a chained stream of its own.
func TestPGLedgerWritesOncePerFact(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := agentactor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	l := &gatecluster.PGLedger{DB: func() eventlog.DB { return pool }}
	body := map[string]any{"subject": "ci-x", "ref": gatecluster.RunRef("ci-x"), "change": gatecluster.ChangeSucceeded,
		"run": "ci-x", "status": "True"}
	for i, want := range []bool{true, false} {
		ok, err := l.Append(ctx, gatecluster.RunStream("ci-x"), gatecluster.TypeRunFinished, 0, func(lastType string, _ map[string]any) (map[string]any, bool) {
			return body, lastType != gatecluster.TypeRunFinished
		})
		if err != nil || ok != want {
			t.Fatalf("write %d: appended %v, %v", i+1, ok, err)
		}
	}
	var n int
	var actor string
	if err := pool.QueryRow(ctx, `SELECT count(*), max(actor_id) FROM agentrt.event WHERE event_stream_id = 'ci:ci-x'`).Scan(&n, &actor); err != nil || n != 1 || actor != gatecluster.Actor {
		t.Fatalf("%d events by %s: %v", n, actor, err)
	}
	if _, err := eventlog.VerifyStream(ctx, pool, gatecluster.RunStream("ci-x")); err != nil {
		t.Fatal(err)
	}
}

// Origin: 2026-11-17, the CI line: a run's cause label holds an event number
// of another ledger when the environments differ. A cause this ledger does
// not hold is left out and the event is still written (the column is a
// foreign key: kept, it would refuse the event); a cause it holds is set.
func TestPGLedgerDropsACauseItDoesNotHold(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := agentactor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	l := &gatecluster.PGLedger{DB: func() eventlog.DB { return pool }}
	body := func(run string) map[string]any {
		return map[string]any{"subject": run, "ref": gatecluster.RunRef(run), "change": gatecluster.ChangeCreated, "run": run}
	}
	if ok, err := l.Append(ctx, gatecluster.RunStream("ci-a"), gatecluster.TypeRunStarted, 999999999, func(string, map[string]any) (map[string]any, bool) {
		return body("ci-a"), true
	}); err != nil || !ok {
		t.Fatalf("an unknown cause lost the event: %v %v", ok, err)
	}
	var known int64
	if err := pool.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event WHERE event_stream_id = 'ci:ci-a'`).Scan(&known); err != nil {
		t.Fatal(err)
	}
	if ok, err := l.Append(ctx, gatecluster.RunStream("ci-b"), gatecluster.TypeRunStarted, known, func(string, map[string]any) (map[string]any, bool) {
		return body("ci-b"), true
	}); err != nil || !ok {
		t.Fatalf("a known cause: %v %v", ok, err)
	}
	var a, b *int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT cause_event_id FROM agentrt.event WHERE event_stream_id = 'ci:ci-a'),
	  (SELECT cause_event_id FROM agentrt.event WHERE event_stream_id = 'ci:ci-b')`).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != nil || b == nil || *b != known {
		t.Fatalf("causes: unknown %v, known %v (want nil, %d)", a, b, known)
	}
}

// 2026-11-16: bus step 2a (RULES.md §7: "register line + Go vocabulary, tests
// aligned"; bus/hook's own check sees only the types linked into its test):
// gate-cluster's three types are registered with gate-cluster as publisher,
// each body requires the fixed fields the entry promises, and the refs the
// gate writes are the register's with the placeholder filled.
func TestGateClusterTypesAgreeWithTheRegister(t *testing.T) {
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{
		gatecluster.TypeRunStarted:    gatecluster.RunRef("ci-platform-x"),
		gatecluster.TypeRunFinished:   gatecluster.RunRef("ci-platform-x"),
		gatecluster.TypeWindowChanged: gatecluster.WindowRef,
	}
	for typ, ref := range refs {
		ch, ok := all["rocket"].Channels[typ]
		if !ok || ch.Publisher != "gate-cluster" {
			t.Errorf("%s: registered %v, publisher %q", typ, ok, ch.Publisher)
			continue
		}
		fields, ok := eventlog.BodyOf(typ)
		if !ok {
			t.Errorf("%s has no declared body", typ)
			continue
		}
		required := map[string]bool{}
		for _, f := range fields {
			required[f.Name] = !f.Optional
		}
		need := []string{"subject", "ref", "change"}
		if ch.Key != "" {
			need = append(need, "key", ch.Key)
		}
		for _, n := range need {
			if !required[n] {
				t.Errorf("%s: the body does not require %q", typ, n)
			}
		}
		pattern := "^" + regexp.MustCompile(`\\\{[a-z]+\\\}`).ReplaceAllString(regexp.QuoteMeta(ch.Ref), `[A-Za-z0-9._-]+`) + "$"
		if !regexp.MustCompile(pattern).MatchString(ref) {
			t.Errorf("%s: ref %q is not the register's %q", typ, ref, ch.Ref)
		}
	}
}
