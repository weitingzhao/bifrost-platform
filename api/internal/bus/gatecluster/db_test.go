package gatecluster_test

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store" // the actor_registered vocabulary and the seed, in the test only
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
	if len(types) != 36 {
		t.Fatalf("the register has %d types, the migration was written for 36", len(types))
	}
	for _, typ := range types {
		if _, err := conn.Exec(ctx, `INSERT INTO vocab VALUES ($1)`, typ); err != nil {
			t.Errorf("%s is refused: %v", typ, err)
		}
	}
	// The negative control: the constraint still refuses what is not registered.
	if _, err := conn.Exec(ctx, `INSERT INTO vocab VALUES ('decision.entry.recorded')`); err == nil {
		t.Error("an unregistered type is taken")
	}
}

// 2026-11-16: bus step 2a (RULES.md §7): the event log takes gate-cluster's
// events from its registered actor, a finish is written once per run, and
// each run is a chained stream of its own.
func TestPGLedgerWritesOncePerFact(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	l := &gatecluster.PGLedger{DB: func() eventlog.DB { return pool }}
	body := map[string]any{"subject": "ci-x", "ref": gatecluster.RunRef("ci-x"), "change": gatecluster.ChangeSucceeded,
		"run": "ci-x", "status": "True"}
	for i, want := range []bool{true, false} {
		ok, err := l.Append(ctx, gatecluster.RunStream("ci-x"), gatecluster.TypeRunFinished, func(lastType string, _ map[string]any) (map[string]any, bool) {
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
