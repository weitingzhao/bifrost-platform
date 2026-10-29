package store_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Executor pins (W-31 round 4, core blocks C-0 and C-1;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.1–§2.3, §6 T3, T4, T10,
// T11, §7.1).

const (
	coordinatorSeat = "seat:coordinator"
	externalHost    = "coordinator-mbp"
)

var coordinator = store.Writer{Actor: coordinatorSeat, Token: coordinatorSeat}

// pinEnvelope is the dev tenant's standing envelope; withHosts adds the
// external host entry of the coordinator's Mac.
func pinEnvelope(version int, direction string, withHosts bool) string {
	body := `tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude, exec:codex]
hosts: [h1, coordinator-mbp]
`
	if withHosts {
		body += `external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform]
    roles: [implement]
`
	}
	return ruleText("standing-envelope:dev", version, direction, since, until, body)
}

// pinWorld is a kernel world with the coordinator's seat and the external
// host registered, and the envelope with its external host signed.
func pinWorld(t *testing.T) *world {
	t.Helper()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": []string{coordinatorSeat}, "note": "test: the coordinator's seat"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'seat', 'Coordinator seat')`,
			coordinatorSeat)
		return err
	}))
	ok(t, store.RegisterHost(ctx, w.rt, "owner", externalHost, store.HostExternal))
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, pinEnvelope(1, rules.Initial, true))))
	return w
}

// pinnedTask is a task of a batch, pinned to the coordinator's Mac unless
// pin says otherwise.
func (w *world) pinnedTask(ref, pin string) store.PinnedTask {
	return store.PinnedTask{TaskProposal: store.TaskProposal{Ref: ref, PlanNode: s1, Role: "implement", Class: "go", Title: ref,
		SpecArtifact: w.spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}, ExecutorPin: pin, Providers: []string{"anthropic"}}
}

// proposePinned proposes one pinned task, returning its id.
func (w *world) proposePinned(t *testing.T, key string) string {
	t.Helper()
	out, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, key, "M-1", "local_s1", []store.PinnedTask{w.pinnedTask("a", rules.PinCoordinator)})
	ok(t, err)
	return out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
}

// readyPinned proposes a pinned task and moves it to ready.
func (w *world) readyPinned(t *testing.T, key string) string {
	t.Helper()
	id := w.proposePinned(t, key)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", id) }))
	return id
}

// T3 and the rest of §2.2 at the store, each refusal only one change from the
// proposal that succeeds, which comes last: a refused batch leaves no task
// and no event, and a retry with the key is answered only to the writer
// who may pin.
func TestPinOnlyByTheEnvelopesProposer(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	tasks := func() int { return w.count(t, `SELECT count(*) FROM agentrt.task`) }
	events := func() int { return w.count(t, `SELECT count(*) FROM agentrt.event`) }
	pinned := func(edit func(*store.PinnedTask)) []store.PinnedTask {
		p := w.pinnedTask("a", rules.PinCoordinator)
		if edit != nil {
			edit(&p)
		}
		return []store.PinnedTask{p}
	}
	for name, c := range map[string]struct {
		writer store.Writer
		tasks  []store.PinnedTask
	}{
		"relayed (T3)":                   {store.Writer{Actor: coordinatorSeat, Token: coordinatorSeat, Relayed: true}, pinned(nil)},
		"another token as the seat":      {store.Writer{Actor: coordinatorSeat, Token: "operator"}, pinned(nil)},
		"another seat":                   {store.Writer{Actor: store.Seat, Token: store.Seat}, pinned(nil)},
		"a thread":                       {store.Writer{Actor: "thread:local_s1", Token: "operator"}, pinned(nil)},
		"a repository the entry has not": {coordinator, pinned(func(p *store.PinnedTask) { p.Repo = "bifrost-trade-infra" })},
		"another role":                   {coordinator, pinned(func(p *store.PinnedTask) { p.Role = "other" })},
		"another effect":                 {coordinator, pinned(func(p *store.PinnedTask) { p.Effect, p.BaseCommit, p.Repo = "none", "", "" })},
		"another pin":                    {coordinator, pinned(func(p *store.PinnedTask) { p.ExecutorPin = "external:other" })},
	} {
		beforeTasks, beforeEvents := tasks(), events()
		_, err := store.ProposePinnedTasks(ctx, w.rt, c.writer, "k-"+name, "M-1", "local_s1", c.tasks)
		if !errors.Is(err, store.ErrPinNotPermitted) || !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v; want pin_not_permitted", name, err)
		}
		if tasks() != beforeTasks || events() != beforeEvents {
			t.Errorf("%s: the refused batch left %d tasks and %d events", name, tasks()-beforeTasks, events()-beforeEvents)
		}
	}
	// A session header that is no session id is refused as such.
	if _, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k-bad-session", "M-1", "no session!", pinned(nil)); !errors.Is(err, store.ErrInvalid) ||
		errors.Is(err, store.ErrPinNotPermitted) {
		t.Errorf("a bad session header: %v", err)
	}

	// The proposal that succeeds: the pin and the session on the row and its event.
	out, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k1", "M-1", "local_s1", append(pinned(nil), w.pinnedTask("b", "")))
	ok(t, err)
	ids := map[string]string{}
	for _, x := range out["tasks"].([]any) {
		m := x.(map[string]any)
		ids[m["ref"].(string)] = m["task_id"].(string)
	}
	var pin *string
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT t.executor_pin, e.body_json FROM agentrt.task t JOIN agentrt.event e
	  ON e.task_id = t.task_id AND e.event_type = 'task_proposed' WHERE t.task_id = $1`, ids["a"]).Scan(&pin, &body))
	if pin == nil || *pin != rules.PinCoordinator || body["executor_pin"] != rules.PinCoordinator || body["via_session"] != "local_s1" ||
		body["token"] != coordinatorSeat {
		t.Fatalf("pinned task: pin %v, event %v", pin, body)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT t.executor_pin, e.body_json FROM agentrt.task t JOIN agentrt.event e
	  ON e.task_id = t.task_id AND e.event_type = 'task_proposed' WHERE t.task_id = $1`, ids["b"]).Scan(&pin, &body))
	if _, has := body["executor_pin"]; pin != nil || has || body["via_session"] != nil {
		t.Fatalf("the batch's unpinned task: pin %v, event %v", pin, body)
	}
	// A retry gets the first answer; the same key from a writer who may not
	// pin gets nothing; the same key without the pin is another request.
	again, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k1", "M-1", "local_s1", append(pinned(nil), w.pinnedTask("b", "")))
	ok(t, err)
	if fmt.Sprint(again) != fmt.Sprint(out) {
		t.Fatalf("a retry: %v, first %v", again, out)
	}
	if _, err := store.ProposePinnedTasks(ctx, w.rt, store.Writer{Actor: coordinatorSeat, Token: coordinatorSeat, Relayed: true}, "k1", "M-1",
		"local_s1", append(pinned(nil), w.pinnedTask("b", ""))); !errors.Is(err, store.ErrPinNotPermitted) {
		t.Fatalf("a relayed retry of the key: %v", err)
	}
	if _, err := store.ProposeTasks(ctx, w.rt, coordinator, "k1", "M-1", []store.TaskProposal{pinned(nil)[0].TaskProposal,
		w.pinnedTask("b", "").TaskProposal}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("the key without the pin: %v", err)
	}
}

// T2 at the store: the envelope's entry is the authority, and only in a
// version the Owner signed; dropping it is a tightening anyone may make, and
// stops new pins at once. An unsigned tightening that keeps it stops them
// too until he signs again (the strict reading of §2.1).
func TestPinNeedsTheSignedEntry(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	w.proposePinned(t, "before")
	_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, true)))
	ok(t, err)
	if _, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "unsigned", "M-1", "", []store.PinnedTask{w.pinnedTask("a", rules.PinCoordinator)}); !errors.Is(err, store.ErrPinNotPermitted) ||
		!strings.Contains(err.Error(), "no signed") {
		t.Fatalf("under an unsigned tightening: %v", err)
	}
	w2 := pinWorld(t)
	w2.proposePinned(t, "before")
	_, err = store.TightenRule(ctx, w2.rt, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, false)))
	ok(t, err)
	if _, err := store.ProposePinnedTasks(ctx, w2.rt, coordinator, "dropped", "M-1", "", []store.PinnedTask{w2.pinnedTask("a", rules.PinCoordinator)}); !errors.Is(err, store.ErrPinNotPermitted) {
		t.Fatalf("with external_hosts dropped: %v", err)
	}
}

// §2.2: what builds a task anywhere but the tasks route has no pin to set.
// TaskProposal (goals, briefs, plans, imports) and Task (the controller's
// checks and gates) export no pin and no session: only PinnedTask does.
func TestOnlyPinnedTaskCarriesAPin(t *testing.T) {
	t.Parallel()
	for _, v := range []any{store.TaskProposal{}, store.Task{}} {
		ty := reflect.TypeOf(v)
		for i := range ty.NumField() {
			f := ty.Field(i)
			if f.IsExported() && (strings.Contains(strings.ToLower(f.Name), "pin") || strings.Contains(strings.ToLower(f.Name), "session")) {
				t.Errorf("%s exports %s", ty.Name(), f.Name)
			}
		}
	}
	if _, ok := reflect.TypeOf(store.PinnedTask{}).FieldByName("ExecutorPin"); !ok {
		t.Fatal("PinnedTask has no ExecutorPin")
	}
}

// T4 (database): no write changes a task's pin, the runtime role's least of
// all; another column of the same row changes with its event. A pinned row
// is made only by its task_proposed event recording that pin, and only on
// an implementation with a branch effect.
func TestPinNeverChanges(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	id := w.proposePinned(t, "k")
	update := func(set string) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
				MissionID: "M-1", PlanNode: s1, TaskID: id, Body: map[string]any{"from": "proposed", "to": "proposed", "reason": "test"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.task SET `+set+` WHERE task_id = $1`, id)
			return err
		})
	}
	ok(t, update(`title = 'renamed'`))
	wantErr(t, update(`executor_pin = NULL`), "keeps its executor pin")
	wantErr(t, update(`title = 'again', executor_pin = NULL`), "keeps its executor pin")

	insert := func(eventPin any, role string) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			task := store.NewTaskID()
			body := map[string]any{"title": "direct", "role": role}
			if eventPin != nil {
				body["executor_pin"], body["via_session"] = eventPin, ""
			}
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_proposed", Actor: "controller",
				MissionID: "M-1", PlanNode: s1, TaskID: task, Body: body}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, off_plan, role, task_class, title,
			  spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id, repo, base_commit, executor_pin)
			  VALUES ($1, 'M-1', $2, false, $3, 'go', 'direct', $4, 'branch', 'proposed', 3600, 'controller', 'bifrost-platform', $5, $6)`,
				task, s1, role, w.spec, commitA, rules.PinCoordinator)
			return err
		})
	}
	ok(t, insert(rules.PinCoordinator, "implement"))
	wantErr(t, insert(nil, "implement"), "its task_proposed event records none")
	wantErr(t, insert(rules.PinCoordinator, "other"), "task_executor_pin_check")
}

// T11 (database): the runtime role leases a pinned task only on the external
// host, and the external host only a pinned task; an attempt with no host
// is on no external host. Refusals first, then the leases that succeed, so
// no refusal is a live attempt's unique index.
func TestPinnedTaskLeasesOnlyOnTheExternalHost(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	pinned := w.proposePinned(t, "k")
	plain := w.ready(t, store.Task{})
	lease := func(task, host string) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, _, err := store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:claude", host, 1, 2*time.Minute)
			return err
		})
	}
	wantErr(t, lease(pinned, "h1"), "runs only on the external host")
	wantErr(t, lease(pinned, ""), "runs only on the external host")
	wantErr(t, lease(plain, externalHost), "runs only on the external host")
	ok(t, lease(pinned, externalHost))
	ok(t, lease(plain, "h1"))
}

// 0010: one external host at most, and a host keeps its kind.
func TestOneExternalHostThatKeepsItsKind(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	wantErr(t, store.RegisterHost(ctx, w.rt, "owner", "another-mac", store.HostExternal), "host_one_external")
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h9", "vm"))
	update := func(host, set string) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: "host:" + host, Type: "host_state_changed", Actor: "owner",
				Body: map[string]any{"host_id": host, "from": "active", "to": "draining"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.host SET `+set+` WHERE host_id = $1`, host)
			return err
		})
	}
	wantErr(t, update("h1", `host_kind = 'external'`), "keeps its kind")
	wantErr(t, update(externalHost, `host_kind = 'dev'`), "keeps its kind")
	ok(t, update("h1", `state = 'draining'`))
}

// T10: an ordinary host never leases a pinned task, and a pinned task never
// keeps it from the tasks after it, in whatever order they were proposed
// (rounds of a random order, the oldest ready task always a pinned one).
// Without the claim's executor_pin IS NULL the first claim would pick the
// pinned task and the attempt guard would refuse it.
func TestOrdinaryClaimsSkipPinnedTasks(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	r := rand.New(rand.NewPCG(uint64(seed), 0))
	host := 8
	for round := range 4 {
		kinds := []bool{true, true, true, false, false, false, false}
		r.Shuffle(len(kinds), func(i, j int) { kinds[i], kinds[j] = kinds[j], kinds[i] })
		kinds = append([]bool{true}, kinds...)
		var want, pins []string
		for i, pin := range kinds {
			if pin {
				pins = append(pins, w.readyPinned(t, fmt.Sprintf("r%d-%d", round, i)))
			} else {
				want = append(want, w.ready(t, store.Task{}))
			}
		}
		var got []string
		for {
			host++
			h := fmt.Sprintf("h%d", host)
			ok(t, store.RegisterHost(ctx, w.rt, "owner", h, "dev"))
			c := w.claim(t, h, time.Hour, claude, codex)
			if c == nil {
				break
			}
			got = append(got, c.TaskID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("round %d: ordinary hosts leased %v; want the unpinned tasks %v in order (pinned %v)", round, got, want, pins)
		}
		if n := w.count(t, `SELECT count(*) FROM agentrt.attempt WHERE task_id = ANY($1)`, pins); n != 0 {
			t.Fatalf("round %d: %d attempts of pinned tasks", round, n)
		}
	}
}

// §7.2 route replay: a pinned task's route input records its pin, so its
// fingerprint differs from an unpinned task's and replays to itself; an
// unpinned task's record is what it was before pins.
func TestRouteRecordsThePin(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	pinned := w.readyPinned(t, "k")
	plain := w.ready(t, store.Task{})
	inputs := func(task string) map[string]any {
		var body map[string]any
		ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE task_id = $1 AND event_type = 'route_decided'
		  ORDER BY event_id DESC LIMIT 1`, task).Scan(&body))
		in := body["inputs"].(map[string]any)
		_, _, fp, err := store.ReplayRoute(in)
		ok(t, err)
		if fp != body["fingerprint"] {
			t.Fatalf("task %s replays to %s, recorded %v", task, fp, body["fingerprint"])
		}
		return in
	}
	if got := inputs(pinned)["executor_pin"]; got != rules.PinCoordinator {
		t.Fatalf("pinned route input names pin %v", got)
	}
	if _, has := inputs(plain)["executor_pin"]; has {
		t.Fatal("an unpinned route input names a pin")
	}
}

// Presence (§7.2): the external host's silence is no news, and it makes no
// executor available; an ordinary host's silence is reported, as before.
func TestExternalHostIsNotReportedOffline(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	ok(t, store.RegisterHost(ctx, w.rt, "owner", externalHost, store.HostExternal))
	w.poll(t, "h1", codex)
	// The external host's agentd claims only by id: what it offers and its
	// contact, recorded as its exact claims record them.
	_, err := store.RecordHostOffered(ctx, w.rt, externalHost, []store.Tool{claude}, false)
	ok(t, err)
	ok(t, store.TouchHost(ctx, w.rt, externalHost))
	var last time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT last_poll_at FROM agentrt.host WHERE host_id = $1`, externalHost).Scan(&last))
	doc, _ := w.presence(t)
	for _, e := range doc.Executors {
		if slices.Contains(e.Hosts, externalHost) {
			t.Fatalf("%s is offered by the external host: %+v", e.Executor, e)
		}
	}
	presenceAt(t, w, last.Add(10*time.Minute))
	alerts, err := w.rt.Query(ctx, `SELECT event_stream_id FROM agentrt.event WHERE event_type = 'policy_alert' AND body_json->>'rule' = 'host_offline'`)
	ok(t, err)
	offline, err := pgx.CollectRows(alerts, pgx.RowTo[string])
	ok(t, err)
	if !slices.Equal(offline, []string{store.HostPresenceStream("h1")}) {
		t.Fatalf("hosts reported offline: %v; want only h1", offline)
	}
}

// An envelope's external host entry names what is there: never a host
// registered as another kind, nor a seat or executor not registered. A host
// not registered yet may be named. Refusals first, each one change from the
// envelope that signs, which comes last.
func TestEnvelopeEntryNamesWhatIsThere(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": []string{coordinatorSeat}, "note": "test: the coordinator's seat"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'seat', 'Coordinator seat')`,
			coordinatorSeat)
		return err
	}))
	s := signer{t, rptest.NewKey(t)}
	text := strings.Replace(pinEnvelope(1, rules.Initial, true), "hosts: [h1, coordinator-mbp]", "hosts: [h1]", 1)
	for name, edit := range map[string][3]string{
		"an ordinary host":      {"host: coordinator-mbp", "host: h2", "registered as a host of another kind"},
		"a seat not registered": {"proposer: seat:coordinator", "proposer: seat:nobody", "not a registered seat"},
		"an executor not there": {"executor: exec:claude", "executor: exec:gemini", "not registered"},
	} {
		_, err := s.sign(w, strings.Replace(text, edit[0], edit[1], 1))
		if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), edit[2]) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok(t, firstErr(s.sign(w, text)))
}
