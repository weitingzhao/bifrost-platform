package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Minimal in-memory DB contract for exercising the real collector; this is
// deliberately not a claim that PostgreSQL integration has been tested.
type collectorTestDB struct {
	mu      sync.Mutex
	now     time.Time
	doc     *planview.Document
	obs     map[string]planObservation
	pass    *planObservationPass
	events  int64
	streams map[string]int64
	active  int
}
type collectorTestTx struct {
	pgx.Tx
	db     *collectorTestDB
	locked bool
	closed bool
}
type collectorTestRow func(...any) error

func (f collectorTestRow) Scan(x ...any) error { return f(x...) }
func (d *collectorTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.mu.Lock()
	d.active++
	d.mu.Unlock()
	return &collectorTestTx{db: d}, nil
}
func (t *collectorTestTx) Commit(context.Context) error {
	if t.closed {
		return nil
	}
	if !t.locked {
		t.db.mu.Lock()
	}
	t.db.active--
	t.closed = true
	t.locked = false
	t.db.mu.Unlock()
	return nil
}
func (t *collectorTestTx) Rollback(c context.Context) error { return t.Commit(c) }
func (t *collectorTestTx) Exec(_ context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "pg_advisory_xact_lock") && strings.HasPrefix(a[0].(string), "plan-observation-pass:") {
		t.db.mu.Lock()
		t.locked = true
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (t *collectorTestTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	return collectorTestRow(func(x ...any) error {
		d := t.db
		if !t.locked {
			d.mu.Lock()
			defer d.mu.Unlock()
		}
		switch {
		case strings.Contains(q, "COALESCE((SELECT last_seq"):
			*x[0].(*int64) = d.streams[a[0].(string)]
			*x[1].(*int64) = d.streams[a[1].(string)]
			return nil
		case strings.HasPrefix(q, "WITH views AS"):
			obs := []planObservation{}
			for _, v := range d.obs {
				obs = append(obs, v)
			}
			passes := []planObservationPass{}
			if d.pass != nil {
				passes = append(passes, *d.pass)
			}
			body, e := json.Marshal(d.doc)
			if e != nil {
				return e
			}
			raw, e := json.Marshal(map[string]any{"now": d.now, "views": []any{map[string]any{"root": d.doc.Root, "tenant": d.doc.Tenant, "plan_version": d.doc.Version}}, "docs": []any{map[string]any{"root": d.doc.Root, "body": body}}, "observations": obs, "passes": passes, "current": map[string]any{}})
			*x[0].(*[]byte) = raw
			return e
		case strings.Contains(q, "SELECT clock_timestamp()"):
			*x[0].(*time.Time) = d.now
			return nil
		case strings.Contains(q, "SELECT body_json->'plan_observation'"):
			return pgx.ErrNoRows
		case strings.Contains(q, "SELECT last_seq, last_sha256"):
			*x[0].(*int64) = d.streams[a[0].(string)]
			*x[1].(*string) = ""
			return nil
		case strings.Contains(q, "INSERT INTO agentrt.event\n"):
			var b map[string]json.RawMessage
			if e := json.Unmarshal([]byte(a[9].(string)), &b); e != nil {
				return e
			}
			if raw := b["plan_observation"]; raw != nil {
				var n planObservation
				if e := json.Unmarshal(raw, &n); e != nil {
					return e
				}
				d.obs[n.Digest] = n
			}
			if raw := b["plan_observation_pass"]; raw != nil {
				var p planObservationPass
				if e := json.Unmarshal(raw, &p); e != nil {
					return e
				}
				d.pass = &p
			}
			d.streams[a[0].(string)]++
			d.events++
			*x[0].(*int64) = d.events
			return nil
		default:
			return fmt.Errorf("unexpected SQL: %s", q)
		}
	})
}
func collectorTestFixture(t *testing.T) (*collectorTestDB, planview.Binding) {
	raw, e := os.ReadFile("../planview/testdata/r4-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	d, e := planview.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	d.Items = d.Items[:1]
	d.Items[0].Dependencies = []planview.Edge{}
	n := &d.Items[0]
	a := n.Acceptance[0]
	b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: "collectorTest", Version: d.Version, Tenant: "dev", Root: d.Root, Node: n.Node, Step: "R4:4.1-1", Acceptance: a.ID, AcceptanceSHA256: planview.AcceptanceDigest(a), Atom: "main_effective", Source: "git", Repo: "repo", Result: strings.Repeat("a", 40)}
	raw, _ = json.Marshal(b)
	n.Bindings = []json.RawMessage{raw}
	if e = d.Validate(); e != nil {
		t.Fatal(e)
	}
	return &collectorTestDB{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), doc: d, obs: map[string]planObservation{}, streams: map[string]int64{}}, b
}
func collectorTestSource(state landing.History) PlanSources {
	return PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: state, Commit: strings.Repeat("a", 40)}, nil
	}}
}

func TestPV2CollectorOutOfOrder(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"new streams", "observation only", "pass only"} {
		t.Run(mode, func(t *testing.T) {
			d, b := collectorTestFixture(t)
			oldState, newState, want := landing.Effective, landing.Reverted, "false"
			if mode != "new streams" {
				collectorTestPoll(t, d, collectorTestSource(landing.Effective), false)
				d.now = d.now.Add(time.Minute)
			}
			if mode == "pass only" {
				d.now = d.now.Add(9 * time.Minute)
				oldState, newState, want = landing.Reverted, landing.Effective, "true"
			}
			started, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			old := PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
				close(started)
				<-release
				return landing.MainHistory{State: oldState, Commit: b.Result}, nil
			}}
			go func() { _, e := CollectPlanObservations(context.Background(), d, old); done <- e }()
			<-started
			beforePass, beforeObservation := d.streams[passStream(b.Root)], d.streams[observationStream(b.Root)]
			if _, e := CollectPlanObservations(context.Background(), d, collectorTestSource(newState)); e != nil {
				t.Fatal(e)
			}
			if mode == "pass only" && (d.streams[passStream(b.Root)] != beforePass+1 || d.streams[observationStream(b.Root)] != beforeObservation) {
				t.Fatal("expected only pass to advance")
			}
			if mode == "observation only" && (d.streams[passStream(b.Root)] != beforePass || d.streams[observationStream(b.Root)] != beforeObservation+1) {
				t.Fatal("expected only observation to advance")
			}
			committed := d.events
			t.Logf("newer reader committed value=%s version=%d", d.obs[b.Digest()].Value, d.obs[b.Digest()].SourceVersion)
			close(release)
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			got := d.obs[b.Digest()]
			t.Logf("older reader committed value=%s version=%d", got.Value, got.SourceVersion)
			if got.Value != want || d.events != committed {
				t.Fatalf("older read overwrote newer committed answer: %+v (events %d -> %d)", got, committed, d.events)
			}
		})
	}
}

func collectorTestAddBinding(t *testing.T, d *collectorTestDB, b planview.Binding, id string) planview.Binding {
	t.Helper()
	b.ID, b.Repo = id, id
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	d.doc.Items[0].Bindings = append(d.doc.Items[0].Bindings, raw)
	return b
}

func collectorTestPoll(t *testing.T, d *collectorTestDB, src PlanSources, fails bool) int {
	t.Helper()
	n, err := CollectPlanObservations(context.Background(), d, src)
	if (err != nil) != fails {
		t.Fatalf("collector count=%d error=%v; failure wanted=%v", n, err, fails)
	}
	return n
}

// Work 632: review631's logical-clock delay probe. The timestamp is before
// each read, even when that read and later bindings take minutes to finish.
func TestPV2CollectorStampsBeforeRemoteRead(t *testing.T) {
	t.Parallel()
	d, b := collectorTestFixture(t)
	b2 := collectorTestAddBinding(t, d, b, "second")
	start := d.now
	calls := 0
	src := PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		if d.active != 0 {
			t.Fatal("remote read ran inside a transaction")
		}
		calls++
		d.now = d.now.Add(time.Minute)
		return landing.MainHistory{State: landing.Effective, Commit: b.Result}, nil
	}}
	collectorTestPoll(t, d, src, false)
	for i, binding := range []planview.Binding{b, b2} {
		want := timeText(start.Add(time.Duration(i) * time.Minute))
		if got := d.obs[binding.Digest()].LastSuccess; got != want {
			t.Errorf("binding %d stamped %s; want %s", i, got, want)
		}
		if got := d.pass.Fresh[binding.Digest()]; got != want {
			t.Errorf("pass binding %d stamped %s; want %s", i, got, want)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestPV2CollectorBoundsRootReads(t *testing.T) {
	t.Parallel()
	d, b := collectorTestFixture(t)
	for i := 1; i <= 15; i++ {
		collectorTestAddBinding(t, d, b, fmt.Sprintf("slow-%d", i))
	}
	start, calls := d.now, 0
	src := PlanSources{History: func(ctx context.Context, _, _, _, _ string) (landing.MainHistory, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Minute {
			t.Error("missing root deadline")
		}
		calls++
		if calls == 1 {
			return landing.MainHistory{State: landing.Effective, Commit: b.Result}, nil
		}
		d.now = d.now.Add(2 * time.Minute)
		return landing.MainHistory{}, errors.New("timeout")
	}}
	collectorTestPoll(t, d, src, true)
	if calls != 4 {
		t.Fatalf("read %d bindings; remaining bindings must be skipped after the 5 minute budget", calls)
	}
	if len(d.obs) != 1 || len(d.pass.Fresh) != 1 || d.obs[b.Digest()].LastSuccess != timeText(start) {
		t.Fatalf("unread bindings written or success restamped: %+v %+v", d.obs, d.pass)
	}
}

// Work 632 changes the promise to 25 minutes from the last persisted success.
// The unpersisted +9m read is deliberately forgotten; +10m failures must not
// write a pass or throttle the next successful refresh.
func TestPV2CollectorPersistedFreshness(t *testing.T) {
	t.Parallel()
	d, b := collectorTestFixture(t)
	start := d.now
	offline := PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{}, errors.New("offline")
	}}
	collectorTestPoll(t, d, offline, true)
	if d.events != 0 {
		t.Error("first all-failed round wrote a pass")
	}
	collectorTestPoll(t, d, collectorTestSource(landing.Effective), false)
	before := d.events
	d.now = start.Add(9 * time.Minute)
	collectorTestPoll(t, d, collectorTestSource(landing.Effective), false)
	if d.events != before {
		t.Fatal("unchanged +9m success was not coalesced")
	}
	for _, minute := range []int{10, 26} {
		d.now = start.Add(time.Duration(minute) * time.Minute)
		collectorTestPoll(t, d, offline, true)
		if d.events != before {
			t.Errorf("all-failed +%dm round wrote events", minute)
		}
	}
	var s *planFactsSnapshot
	if err := InTx(context.Background(), d, func(tx pgx.Tx) error { var e error; s, e = readPlanFacts(context.Background(), tx); return e }); err != nil {
		t.Fatal(err)
	}
	if _, status, o := s.reading(b); status != "Stale" || o.LastSuccess != timeText(start) {
		t.Fatalf("persisted freshness: %s %+v", status, o)
	}
	collectorTestPoll(t, d, collectorTestSource(landing.Effective), false)
	if d.events != before+1 || d.pass.Fresh[b.Digest()] != timeText(d.now) {
		t.Fatal("recovery failed to refresh immediately")
	}
}

func TestPV2CollectorRefreshesEachPersistedSuccess(t *testing.T) {
	t.Parallel()
	d, b := collectorTestFixture(t)
	b2 := collectorTestAddBinding(t, d, b, "second")
	start := d.now
	src := func(failedRepo string) PlanSources {
		return PlanSources{History: func(_ context.Context, repo, _, _, _ string) (landing.MainHistory, error) {
			if repo == failedRepo {
				return landing.MainHistory{}, errors.New("offline")
			}
			return landing.MainHistory{State: landing.Effective, Commit: b.Result}, nil
		}}
	}
	collectorTestPoll(t, d, src(b2.Repo), true)
	d.now = start.Add(9 * time.Minute)
	collectorTestPoll(t, d, src(b.Repo), true) // second binding first succeeds; pass moves to +9m
	if d.pass.At != timeText(d.now) {
		t.Fatal("binding set change did not write pass")
	}
	before := d.events
	d.now = start.Add(10 * time.Minute)
	collectorTestPoll(t, d, src(b2.Repo), true)
	if d.events != before+1 || d.pass.Fresh[b.Digest()] != timeText(d.now) {
		t.Fatal("other binding's recent pass postponed a due refresh")
	}
	if d.pass.Fresh[b2.Digest()] != timeText(start.Add(9*time.Minute)) {
		t.Fatal("failure forgot persisted success")
	}
}
