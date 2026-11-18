package planstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

// The refusals of the plan code's ports (planports.go), each shown on a port
// value the test gives, so the package's own bindings are not touched. That
// each port is bound, and to what, is the binder's test (the store's
// planports_test.go).

// unboundRoot is a plan root's id; no port here reads it.
const unboundRoot = "R2"

// unboundOp is the op of a recorder import's proposal, the recorder
// import's own word (its OpRecorderImport); no port here reads it either.
const unboundOp = "recorder_import"

// refused holds an error to the refusal of an unbound port.
func refused(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrPortUnbound) {
		t.Fatalf("%s, unbound: %v; want ErrPortUnbound", what, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the plan code reads the rules in force through a port, and a port nobody bound must refuse the proposal or the card, never answer "no rule in force" (which would send every change to the Owner's card, or ask every card of the Owner, with nothing saying why).
func TestUnboundRulePortsRefuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, p := range map[string]*RulePorts{"nothing bound": nil, "an empty binding": {}} {
		r, err := planRuleVia(ctx, p, nil)
		refused(t, name+": the plan-standing rule", err)
		if r != nil {
			t.Fatalf("%s: a rule with the refusal: %+v", name, r)
		}
		kinds, err := cardAutoKindsVia(ctx, p, nil)
		refused(t, name+": the card-auto-answer rule", err)
		if kinds != nil {
			t.Fatalf("%s: kinds with the refusal: %v", name, kinds)
		}
	}
	// The positive control: a bound port is called and its answer returned.
	want := &PlanRule{ID: "plan-standing", Version: 7, DoneRules: true}
	bound := &RulePorts{
		PlanRuleInForce:      func(context.Context, pgx.Tx) (*PlanRule, error) { return want, nil },
		CardAutoKindsInForce: func(context.Context, pgx.Tx) ([]string, error) { return []string{"blocked"}, nil },
	}
	if r, err := planRuleVia(ctx, bound, nil); err != nil || r != want {
		t.Fatalf("a bound port: %+v %v", r, err)
	}
	if kinds, err := cardAutoKindsVia(ctx, bound, nil); err != nil || !reflect.DeepEqual(kinds, []string{"blocked"}) {
		t.Fatalf("a bound port: %v %v", kinds, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the plan code checks external work's note and description and reads a plan's registered work through ports, and decides a recorder import through two more; unbound, each must refuse (the note or artifact is not stored, the plan view and the decision fail), never pass a record unchecked, show a plan with no external work, or decide a recorder import as a plan change.
func TestUnboundLineagePortsRefuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, p := range map[string]*ExternalWorkPorts{"nothing bound": nil, "an empty binding": {}} {
		refused(t, name+": the note check", checkExternalNoteVia(p, map[string]any{}))
		refused(t, name+": the description check", checkExternalDescriptionVia(p, []byte(`{}`)))
		work, err := externalWorkSnapshotVia(ctx, p, nil, unboundRoot, time.Now())
		refused(t, name+": the snapshot", err)
		working, finished, err := externalActivityBucketsVia(p, nil, time.Now(), func(string) any { return nil })
		refused(t, name+": the buckets", err)
		if work != nil || working != nil || finished != nil {
			t.Fatalf("%s: an answer with the refusal: %v %v %v", name, work, working, finished)
		}
		prefix, err := collectorActorPrefixVia(p)
		refused(t, name+": the collector's actor prefix", err)
		if prefix != "" {
			t.Fatalf("%s: a prefix with the refusal: %q", name, prefix)
		}
		// Origin: 2026-11-18, TD-582 (inventory §2.3): the plan's reads of the work's records moved behind these ports; unbound, Recent changes must not read as a plan without work, the coverage count must not call every thread unregistered, and a plan with work must not lose its dev boundary.
		streams, err := externalRecentStreamsVia(ctx, p, nil, unboundRoot)
		refused(t, name+": the recent streams", err)
		events, err := externalRecentEventsVia(ctx, p, nil, unboundRoot, []byte(`{}`), 0, 0, 21)
		refused(t, name+": the recent events", err)
		has, err := externalHasWorkVia(ctx, p, nil, unboundRoot)
		refused(t, name+": whether the plan has work", err)
		registrants, err := externalRegistrantsVia(ctx, p, nil, unboundRoot)
		refused(t, name+": the registrants", err)
		if streams != nil || events != nil || has || registrants != nil {
			t.Fatalf("%s: an answer with the refusal: %v %v %v %v", name, streams, events, has, registrants)
		}
	}
	for name, p := range map[string]*RecorderImportPorts{"nothing bound": nil, "an empty binding": {}} {
		refused(t, name+": the landing", decideRecorderImportLogVia(ctx, p, nil, "sha256:log", 1))
	}
	// The positive control: bound ports are called and their answers returned.
	bound := &ExternalWorkPorts{
		CheckNote:        func(any) error { return errors.New("note") },
		CheckDescription: func([]byte) error { return errors.New("description") },
		Snapshot: func(context.Context, pgx.Tx, string, time.Time) ([]map[string]any, error) {
			return []map[string]any{{"external_work_id": "ew"}}, nil
		},
		ActivityBuckets: func(work []map[string]any, _ time.Time, _ func(string) any) ([]any, []any) {
			return []any{len(work)}, []any{"done"}
		},
		RecentStreams: func(_ context.Context, _ pgx.Tx, root string) (map[string]int64, error) {
			return map[string]int64{root: 4}, nil
		},
		RecentEvents: func(_ context.Context, _ pgx.Tx, root string, bounds []byte, before, after int64, limit int) ([]planreadings.RecentEvent, error) {
			return []planreadings.RecentEvent{{Raw: json.RawMessage(root + string(bounds)), ID: before + after + int64(limit)}}, nil
		},
		HasWork:              func(_ context.Context, _ pgx.Tx, root string) (bool, error) { return root == unboundRoot, nil },
		Registrants:          func(_ context.Context, _ pgx.Tx, root string) ([]string, error) { return []string{root}, nil },
		CollectorActorPrefix: "thread:c@",
	}
	// Origin: 2026-11-18, TD-582: the positive control of the four refusals above: bound, each port gets what the plan gave and its answer comes back.
	if streams, err := externalRecentStreamsVia(ctx, bound, nil, unboundRoot); err != nil || !reflect.DeepEqual(streams, map[string]int64{unboundRoot: 4}) {
		t.Fatalf("bound recent streams: %v %v", streams, err)
	}
	if events, err := externalRecentEventsVia(ctx, bound, nil, unboundRoot, []byte(`{}`), 1, 2, 21); err != nil || len(events) != 1 || string(events[0].Raw) != unboundRoot+`{}` || events[0].ID != 24 {
		t.Fatalf("bound recent events: %v %v", events, err)
	}
	if has, err := externalHasWorkVia(ctx, bound, nil, unboundRoot); err != nil || !has {
		t.Fatalf("bound has work: %v %v", has, err)
	}
	if names, err := externalRegistrantsVia(ctx, bound, nil, unboundRoot); err != nil || !reflect.DeepEqual(names, []string{unboundRoot}) {
		t.Fatalf("bound registrants: %v %v", names, err)
	}
	if err := checkExternalNoteVia(bound, nil); err == nil || err.Error() != "note" {
		t.Fatalf("a bound note check: %v", err)
	}
	if err := checkExternalDescriptionVia(bound, nil); err == nil || err.Error() != "description" {
		t.Fatalf("a bound description check: %v", err)
	}
	if work, err := externalWorkSnapshotVia(ctx, bound, nil, unboundRoot, time.Now()); err != nil || len(work) != 1 {
		t.Fatalf("a bound snapshot: %v %v", work, err)
	}
	if prefix, err := collectorActorPrefixVia(bound); err != nil || prefix != "thread:c@" {
		t.Fatalf("a bound collector's actor prefix: %q %v", prefix, err)
	}
	if working, finished, err := externalActivityBucketsVia(bound, make([]map[string]any, 3), time.Now(), nil); err != nil ||
		!reflect.DeepEqual(working, []any{3}) || !reflect.DeepEqual(finished, []any{"done"}) {
		t.Fatalf("bound buckets: %v %v %v", working, finished, err)
	}
	rec := &RecorderImportPorts{
		Decide: func(context.Context, pgx.Tx, string, int64) error { return errors.New("landed") },
	}
	if err := decideRecorderImportLogVia(ctx, rec, nil, "kept", 1); err == nil || err.Error() != "landed" {
		t.Fatalf("a bound recorder landing: %v", err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): a plan note that says the Owner approved is recorded as a claim through a port to coordination; unbound, the port must refuse so the note is not stored, never keep the note with no record of the claim.
func TestUnboundCoordinationPortRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, p := range map[string]*CoordinationPorts{"nothing bound": nil, "an empty binding": {}} {
		refused(t, name+": the claim record", alertClaimVia(ctx, p, nil, eventlog.Event{Stream: "plan:x"}, 1, "plan_note", "", []byte("Owner 已批准")))
	}
	// The positive control: a bound port is called with what the plan gave.
	var got []any
	bound := &CoordinationPorts{AlertClaim: func(_ context.Context, _ pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error {
		got = []any{ev.Stream, cause, source, artifact, string(body)}
		return errors.New("recorded")
	}}
	err := alertClaimVia(ctx, bound, nil, eventlog.Event{Stream: "plan:x"}, 7, "plan_note", "", []byte("text"))
	if err == nil || err.Error() != "recorded" || !reflect.DeepEqual(got, []any{"plan:x", int64(7), "plan_note", "", "text"}) {
		t.Fatalf("a bound port: %v %v", got, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): a plan import asks the question store, through a port, how large it will count the card's body before asking; unbound, the port must refuse the import, never count the body as fitting.
func TestUnboundQuestionPortRefuses(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*QuestionPorts{"nothing bound": nil, "an empty binding": {}} {
		n, err := cardBodyBytesVia(p, strings.Repeat("a", 5000))
		refused(t, name+": the size of a card's body", err)
		if n != 0 {
			t.Fatalf("%s: a size with the refusal: %d", name, n)
		}
	}
	// The positive control: a bound port's count is returned.
	if n, err := cardBodyBytesVia(&QuestionPorts{BodyBytes: func(s string) int { return len(s) + 2 }}, "abc"); err != nil || n != 5 {
		t.Fatalf("a bound port: %d %v", n, err)
	}
}

// Origin: 2026-11-17, plan core wall step 5 (design duty/from-biz-2026-11-17-plan-core-wall.md): Progress reads the controller loops failing now through a port, so the plan code can leave the store; unbound, the port must refuse the read, never answer that no loop is failing (the Console would show a healthy controller while a loop fails every pass, the state TD-411 was written against).
func TestUnboundLoopPortRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, p := range map[string]*LoopPorts{"nothing bound": nil, "an empty binding": {}} {
		loops, err := controllerLoopsFailingVia(ctx, p, nil)
		refused(t, name+": the loops failing now", err)
		if loops != nil {
			t.Fatalf("%s: loops with the refusal: %v", name, loops)
		}
	}
	// The positive control: a bound port is called and its answer returned.
	bound := &LoopPorts{Failing: func(context.Context, pgx.Tx) ([]map[string]any, error) {
		return []map[string]any{{"loop": "agentrt-facts-rules"}}, nil
	}}
	if loops, err := controllerLoopsFailingVia(ctx, bound, nil); err != nil || len(loops) != 1 || loops[0]["loop"] != "agentrt-facts-rules" {
		t.Fatalf("a bound port: %v %v", loops, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): whether a proposal is a recorder import is the plan's own since the proposal moved into planstore: one change, op recorder_import, naming a log. Moved here from the store's TestRecorderImportPortsAreBoundToTheRecorderImport on 2026-11-18 (TD-582): it is no binding of the store's.
func TestARecorderImportIsItsOneChange(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		changes []PlanChange
		log     string
		is      bool
	}{
		{[]PlanChange{{Op: unboundOp, Artifact: "sha256:log"}}, "sha256:log", true},
		{[]PlanChange{{Op: unboundOp}}, "", false},
		{[]PlanChange{{Op: "retitle", Node: "s1", Title: "x"}}, "", false},
		{[]PlanChange{{Op: unboundOp, Artifact: "a"}, {Op: unboundOp, Artifact: "b"}}, "", false},
		{nil, "", false},
	} {
		log, is, err := recorderImportLog(c.changes)
		if err != nil || log != c.log || is != c.is {
			t.Fatalf("on %+v: %q %v %v, want %q %v", c.changes, log, is, err, c.log, c.is)
		}
	}
}
