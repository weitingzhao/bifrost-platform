package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A commit prefix names one change: a unique prefix resolves to its latest
// landing operation, two changes are ErrInvalid naming both, none is
// pgx.ErrNoRows, and a prefix that is not 7 to 40 lowercase hex is ErrInvalid.
func TestResolveDeliveryCommit(t *testing.T) {
	t.Parallel()
	const other = "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	attempt := store.DeliveryCandidate{Repo: "bifrost-platform", Task: landingTask, Attempt: landingAttempt, Commit: commitA}
	first := store.DeliveryCandidate{Repo: "bifrost-platform", Op: "land:1", Task: landingTask, Attempt: landingAttempt, Commit: commitA, Claim: 1}
	second := store.DeliveryCandidate{Repo: "bifrost-platform", Op: "land:2", Task: landingTask, Attempt: landingAttempt, Commit: commitA, Claim: 2}
	landed := store.DeliveryCandidate{Repo: "bifrost-platform", Op: "land:3", Task: other, Commit: commitB, Claim: 1}
	clash := store.DeliveryCandidate{Repo: "bifrost-platform", Op: "land:4", Task: other, Commit: commitA[:12] + strings.Repeat("0", 28), Claim: 1}
	for _, c := range []struct {
		name, prefix string
		candidates   []store.DeliveryCandidate
		want         string // the operation, "-" for the attempt
		err          error
		mention      []string
	}{
		{name: "unique prefix", prefix: commitB[:7], candidates: []store.DeliveryCandidate{attempt, first, landed}, want: "land:3"},
		{name: "full commit", prefix: commitA, candidates: []store.DeliveryCandidate{landed, clash, first}, want: "land:1"},
		{name: "latest operation of one change", prefix: commitA[:9], candidates: []store.DeliveryCandidate{attempt, second, first}, want: "land:2"},
		{name: "attempt only", prefix: commitA[:7], candidates: []store.DeliveryCandidate{attempt, landed}, want: "-"},
		{name: "ambiguous", prefix: commitA[:12], candidates: []store.DeliveryCandidate{first, clash}, err: store.ErrInvalid, mention: []string{landingTask, other}},
		{name: "none", prefix: "abcdef0", candidates: []store.DeliveryCandidate{first, landed}, err: pgx.ErrNoRows},
		{name: "no candidates", prefix: commitA[:7], err: pgx.ErrNoRows},
		{name: "too short", prefix: commitA[:6], candidates: []store.DeliveryCandidate{first}, err: store.ErrInvalid},
		{name: "too long", prefix: commitA + "0", candidates: []store.DeliveryCandidate{first}, err: store.ErrInvalid},
		{name: "upper case", prefix: strings.ToUpper(commitB[:8]), candidates: []store.DeliveryCandidate{landed}, err: store.ErrInvalid},
		{name: "not hex", prefix: "2333e0z", candidates: []store.DeliveryCandidate{first}, err: store.ErrInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := store.ResolveDeliveryCommitForTest(c.prefix, c.candidates)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("err = %v; want %v", err, c.err)
				}
				for _, m := range c.mention {
					if !strings.Contains(err.Error(), m) {
						t.Errorf("err %q does not name %s", err, m)
					}
				}
				return
			}
			ok(t, err)
			op := got.Op
			if op == "" {
				op = "-"
			}
			if op != c.want {
				t.Fatalf("resolved %+v; want operation %s", got, c.want)
			}
		})
	}
}

// One read of a change's delivery stages, by commit or by task, against the
// migrated database.
func TestReadDelivery(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w := newWorld(t)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := at
	t.Cleanup(freezeClock(t, clock))
	put := func(body map[string]any) {
		t.Helper()
		clock = clock.Add(time.Minute)
		restore := freezeClock(t, clock)
		defer restore()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", Body: body})
			return err
		}))
	}
	land := func(r map[string]any) { put(map[string]any{"text": "Landing protocol test", "landing": r}) }
	land(landingRecord("enable", "enabled", "repo", "bifrost-platform", "rule_version", 1))
	land(landingRecord("land:1", "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1))
	land(landingRecord("land:1", "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339), "dry_run", false))
	ref := "refs/heads/land/prod/" + landingTask + "/1"
	land(landingRecord("land:1", "branch_intent", "ref", ref, "R", commitB))
	land(landingRecord("land:1", "branch_pushed", "ref", ref, "R", commitB))
	land(landingRecord("land:1", "ci_kick", "try", 1))
	kicked := clock
	land(landingRecord("land:1", "ci_seen", "run", "ci-1", "status", "Succeeded", "R", commitB, "created_at", clock.Format(time.RFC3339)))
	green := clock
	func() {
		clock = clock.Add(time.Minute)
		restore := freezeClock(t, clock)
		defer restore()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "landing-quota:dev:" + at.Format("2006-01-02"), Type: "note", Actor: "controller",
				Body: map[string]any{"text": "Landing protocol test", "landing": landingRecord("land:1", "reserved", "root", landingTask)}})
			return err
		}))
	}()
	land(landingRecord("land:1", "main_intent", "R", commitB, "M0", commitA))
	put(map[string]any{"text": "Landing main_result: ok (pushed as lander)", "landing": landingRecord("land:1", "main_result", "outcome", "ok"), "landing_pusher": "lander"})
	land(landingRecord("land:1", "landed", "R", commitB))
	landed := clock

	read := func(q store.DeliveryQuery, cycles []store.ReleaseCycle, cyclesErr error) (map[string]any, error) {
		return store.ReadDelivery(ctx, w.rt, q, cycles, cyclesErr)
	}
	stages := func(t *testing.T, doc map[string]any) map[string]store.DeliveryStage {
		t.Helper()
		list, _ := doc["stages"].([]store.DeliveryStage)
		if len(list) != len(store.DeliveryStageOrder) {
			t.Fatalf("stages %+v", doc["stages"])
		}
		out := map[string]store.DeliveryStage{}
		for i, s := range list {
			if s.Stage != store.DeliveryStageOrder[i] {
				t.Fatalf("stage %d is %s", i, s.Stage)
			}
			out[s.Stage] = s
		}
		return out
	}

	doc, err := read(store.DeliveryQuery{Commit: commitB[:8]}, nil, nil)
	ok(t, err)
	if doc["schema"] != "agentrt.delivery_response/v1" || doc["commit"] != commitB || doc["task_id"] != landingTask || doc["attempt_id"] != landingAttempt ||
		doc["repo"] != "bifrost-platform" || doc["operation"] != "land:1" || doc["as_of"] == "" {
		t.Fatalf("response %+v", doc)
	}
	st := stages(t, doc)
	if s := st[store.StageLandingCI]; s.State != store.StageDone || !s.EnteredAt.Equal(kicked) || !s.LeftAt.Equal(green) {
		t.Errorf("landing CI %+v", s)
	}
	if s := st[store.StageOwnerWait]; s.State != store.StageNotReached || s.Reason != "no grant needed" {
		t.Errorf("owner wait %+v", s)
	}
	if s := st[store.StageOnMain]; s.State != store.StageCurrent || !s.EnteredAt.Equal(landed) || s.Actor != "lander" || s.ActorRole != "pusher" {
		t.Errorf("on main %+v", s)
	}
	for _, name := range []string{store.StageProdRelease, store.StageOnProd} {
		if s := st[name]; s.State != store.StageUnknown {
			t.Errorf("%s with no release cycles: %+v", name, s)
		}
	}
	warnings, _ := doc["warnings"].([]string)
	if !anyContains(warnings, "release cycles") {
		t.Errorf("warnings %q", warnings)
	}

	// The claimed A names the same change; a failed cycle read is a warning.
	doc, err = read(store.DeliveryQuery{Commit: commitA[:7]}, nil, errors.New("promote store down"))
	ok(t, err)
	if doc["operation"] != "land:1" || doc["commit"] != commitA {
		t.Fatalf("by A: %+v", doc)
	}
	if warnings, _ := doc["warnings"].([]string); !anyContains(warnings, "promote store down") {
		t.Errorf("warnings %q", warnings)
	}
	if st := stages(t, doc); st[store.StageOnProd].State != store.StageUnknown {
		t.Errorf("on prod with a failed cycle read: %+v", st[store.StageOnProd])
	}

	// Cycles read and holding nothing: the PROD stages are not unknown for
	// want of cycles.
	doc, err = read(store.DeliveryQuery{Commit: commitB}, []store.ReleaseCycle{}, nil)
	ok(t, err)
	if warnings, _ := doc["warnings"].([]string); anyContains(warnings, "release cycles") {
		t.Errorf("warnings %q", warnings)
	}
	if s := stages(t, doc)[store.StageProdRelease]; s.Reason == "no release cycles given" {
		t.Errorf("prod release %+v", s)
	}

	// A second change claiming the same A makes its prefix ambiguous.
	const other = "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	land(landingRecord("land:2", "claimed", "task", other, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAW", "A", commitA, "rule_version", 1))
	land(landingRecord("land:2", "failed", "why", "declined"))
	_, err = read(store.DeliveryQuery{Commit: commitA[:10]}, nil, nil)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), landingTask) || !strings.Contains(err.Error(), other) {
		t.Fatalf("ambiguous commit: %v", err)
	}
	if doc, err = read(store.DeliveryQuery{Commit: commitB[:7]}, nil, nil); err != nil || doc["operation"] != "land:1" {
		t.Fatalf("R stays unique: %v %+v", err, doc)
	}

	// A task resolves to its latest landing operation.
	task := w.ready(t, store.Task{})
	land(landingRecord("land:3", "claimed", "task", task, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAY", "A", commitC, "rule_version", 1))
	land(landingRecord("land:3", "failed", "why", "declined"))
	land(landingRecord("land:4", "claimed", "task", task, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAZ", "A", commitC, "rule_version", 1))
	doc, err = read(store.DeliveryQuery{Task: task}, nil, nil)
	ok(t, err)
	if doc["operation"] != "land:4" || doc["task_id"] != task || doc["plan_node_id"] != s1 || doc["commit"] != commitC || doc["attempt_id"] != "a_01ARZ3NDEKTSV4RRFFQ69G5FAZ" {
		t.Fatalf("by task: %+v", doc)
	}
	if s := stages(t, doc)[store.StageHandedIn]; s.State != store.StageDone {
		t.Errorf("handed in %+v", s)
	}

	if _, err = read(store.DeliveryQuery{Commit: "abcdef0"}, nil, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown commit: %v", err)
	}
	if _, err = read(store.DeliveryQuery{Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAX"}, nil, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown task: %v", err)
	}
	for _, q := range []store.DeliveryQuery{{}, {Commit: commitB, Task: landingTask}, {Commit: "2333e0"}, {Task: "not-a-task"}} {
		if _, err = read(q, nil, nil); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%+v: %v", q, err)
		}
	}
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
