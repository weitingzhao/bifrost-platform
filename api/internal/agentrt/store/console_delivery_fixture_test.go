package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console reads GET /delivery and GET /plan-timeline with closed parsers
// (console/src/api/deliveryStages.ts, console/src/api/planTimeline.ts), held
// to the answers in the two fixtures below. The handlers write what
// ReadDelivery and ReadRoundTimeline return as it is (writeJSON), so these
// answers are those reads on the migrated database, for a round where one
// task went to PROD, another reached main only, and an external work handed
// in a commit of its own:
//
//	CONSOLE_DELIVERY_WRITE=1 go test ./internal/agentrt/store/ -run TestConsoleDeliveryFixtures
//
// rewrites both fixtures; then run the Console's tests (npx vitest run
// src/api/__tests__/deliveryStages.test.ts src/api/__tests__/planTimeline.test.ts).
// Without the variable the test reads the round again and fails when a key
// the answers carry is missing from the fixtures, or the other way round, so
// a field added in Go goes red here until the fixtures and parsers follow.
const (
	consoleFixtures       = "../../../../console/src/api/__fixtures__"
	consoleDeliveryFile   = "agentrt-delivery.go.json"
	consolePlanTimeline   = "agentrt-plan-timeline.go.json"
	consoleFixtureStgP    = "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a"
	consoleFixtureStgU    = "6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b"
	consoleFixtureOnMainR = "7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c"
)

type consoleFixtureCase struct {
	Name     string          `json:"name"`
	Query    string          `json:"query"`
	Response json.RawMessage `json:"response"`
}

type consoleFixture struct {
	Cases []consoleFixtureCase `json:"cases"`
}

// Serial: FreezeEventClock.
func TestConsoleDeliveryFixtures(t *testing.T) {
	w, c := dbWorld(t)
	attemptOf := func(task string) string {
		t.Helper()
		var id string
		ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1 ORDER BY attempt_no DESC LIMIT 1`, task).Scan(&id))
		return id
	}
	put := func(op, kind string, kv ...any) time.Time {
		t.Helper()
		at := c.dbTick()
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, kind, kv...)))
		return at
	}
	// The STG release protocol refuses new legacy STG records; a stream
	// from before it holds them, and both reads still fold them. They are
	// written as such a stream holds them, past Append.
	legacy := func(op, kind string, kv ...any) {
		t.Helper()
		at := c.dbTick()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			return consoleRawLanding(tx, landingRepoStream, at, landingRecord(op, kind, kv...))
		}))
	}
	land := func(task, r string) string {
		t.Helper()
		op := "land:" + task + ":1"
		put(op, "claimed", "task", task, "attempt", attemptOf(task), "A", commitA, "rule_version", 1)
		prepared := c.dbTick().Add(10 * time.Second)
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", r, "M0", commitA,
			"committer_date", prepared.Format(time.RFC3339Nano), "dry_run", false)))
		ref := "refs/heads/land/prod/" + task + "/1"
		put(op, "branch_intent", "ref", ref, "R", r)
		put(op, "branch_pushed", "ref", ref, "R", r)
		put(op, "ci_seen", "run", "ci-"+task, "status", "Succeeded", "R", r, "created_at", c.at.Add(5*time.Second).Format(time.RFC3339Nano))
		put(op, "main_intent", "R", r, "M0", commitA)
		put(op, "main_result", "outcome", "ok")
		put(op, "landed", "R", r)
		return op
	}

	// Task prod lands R=commitB, STG verifies P carrying it, PROD runs P.
	prod := dbAccepted(t, w, c, s2, "bifrost-platform")
	land(prod, commitB)
	stg := "stg:" + prod + ":1"
	legacy(stg, "stg_pending", "R", []any{commitB})
	legacy(stg, "stg_snapshot", "P", consoleFixtureStgP, "U", consoleFixtureStgU, "try", 1, "not_before", c.at.Format(time.RFC3339Nano))
	legacy(stg, "stg_started", "run", "stg-run-1", "try", 1)
	legacy(stg, "stg_result", "run", "stg-run-1", "outcome", "ok")
	legacy(stg, "stg_verified", "P", consoleFixtureStgP, "U", consoleFixtureStgU,
		"digests", map[string]any{"platform-api": "sha256:" + strings.Repeat("a", 64), "platform-workers": "sha256:" + strings.Repeat("a", 64), "platform-console": "sha256:" + strings.Repeat("c", 64)})
	deployed := c.dbTick()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "deploy:prod", Type: "deploy_seen", Actor: "controller",
			Body: map[string]any{"env": "prod", "commit": consoleFixtureStgP, "deploy_ref": map[string]any{"cluster": "prod", "namespace": "platform"}}})
		return err
	}))

	// A landing card of task prod asked twice: the first asking expires and
	// is closed, the second is answered.
	opts := map[string]any{"a": "try_again", "b": "do_not_merge"}
	seeQuestion := func(id string, q store.Question) {
		t.Helper()
		c.dbTick()
		q.ID = id
		_, err := store.SeeQuestion(ctx, w.rt, id, func(context.Context) (store.Question, bool) { return q, true })
		ok(t, err)
	}
	card := func(name string, body map[string]any) {
		t.Helper()
		c.dbTick()
		ok(t, putCard(w, "controller", cardStream, name, body))
	}
	first, second := store.CardPrepID(store.CardRootFailed, 7, 1), store.CardPrepID(store.CardRootFailed, 7, 2)
	card("landing_card_prepared", preparedCard(prod, store.CardRootFailed, 7, 1, opts))
	card("landing_card_ask_started", map[string]any{"prep": first})
	card("landing_card", map[string]any{"prep": first, "question_id": "q1"})
	seeQuestion("q1", store.Question{Status: "open"})
	seeQuestion("q1", store.Question{Status: "expired"})
	card("landing_card_closed", map[string]any{"prep": first, "question_id": "q1", "why": "expired"})
	card("landing_card_prepared", preparedCard(prod, store.CardRootFailed, 7, 2, opts))
	card("landing_card_ask_started", map[string]any{"prep": second})
	card("landing_card", map[string]any{"prep": second, "question_id": "q2"})
	seeQuestion("q2", store.Question{Status: "open"})
	answeredAt := c.at.Add(10 * time.Second).UTC().Format(time.RFC3339Nano)
	seeQuestion("q2", store.Question{Status: store.QuestionAnswered, Option: "a", AnsweredBy: "owner", AnsweredAt: answeredAt})
	card("landing_card_acked", map[string]any{"prep": second, "question_id": "q2"})

	// Task main lands after, and waits for STG.
	main := dbAccepted(t, w, c, s2, "bifrost-platform")
	land(main, consoleFixtureOnMainR)

	cycle := []store.ReleaseCycle{{Revision: consoleFixtureStgP, ProdDeployStartedAt: deployed.Add(-2 * time.Minute),
		ProdDeployCompletedAt: deployed.Add(-time.Minute), ProdDeployResult: "succeeded", ProdGatePassedAt: deployed.Add(30 * time.Second), TriggeredBy: "releaser"}}
	encode := func(v any) json.RawMessage {
		t.Helper()
		var b bytes.Buffer
		ok(t, json.NewEncoder(&b).Encode(v))
		return bytes.TrimSpace(b.Bytes())
	}
	var delivery consoleFixture
	for _, d := range []struct {
		name, task string
		cycles     []store.ReleaseCycle
		cyclesErr  error
	}{
		{"on PROD by deploy_seen, cycles read and empty", prod, []store.ReleaseCycle{}, nil},
		{"on PROD by the release cycle's gate", prod, cycle, nil},
		{"PROD unknown: the release cycles could not be read", prod, nil, errors.New("gitea: unreachable")},
		{"on main, waiting for STG", main, []store.ReleaseCycle{}, nil},
	} {
		out, err := store.ReadDelivery(ctx, w.rt, store.DeliveryQuery{Task: d.task}, d.cycles, d.cyclesErr)
		ok(t, err)
		delivery.Cases = append(delivery.Cases, consoleFixtureCase{Name: d.name, Query: "task=" + d.task, Response: encode(out)})
	}

	for _, item := range []struct {
		name     string
		evidence store.DeliveryProduction
	}{
		{"on PROD by current release and live workloads", store.DeliveryProduction{Commit: consoleFixtureStgP, StartedAt: deployed.Add(-2 * time.Minute), CompletedAt: deployed.Add(-time.Minute)}},
		{"PROD unknown: current workloads unsettled", store.DeliveryProduction{Reason: "PROD workloads are unavailable or unsettled"}},
	} {
		out, err := store.ReadDelivery(ctx, w.rt, store.DeliveryQuery{Task: prod, Production: &item.evidence}, nil, nil)
		ok(t, err)
		delivery.Cases = append(delivery.Cases, consoleFixtureCase{Name: item.name, Query: "task=" + prod, Response: encode(out)})
	}
	// A commit only an external work names (the main lane): its recorded
	// pipeline runs tell the stages, and the merge into main is unknown.
	externalSubmitted(t, w, "thread:giant", s2, "console-fixture", externalCommit)
	started := c.at.Add(-40 * time.Minute)
	recordRuns(t, w, externalRun("ci-platform-a", "bifrost-ci-platform", "Succeeded", started, 7*time.Minute))
	external := func(name string) {
		t.Helper()
		out, err := store.ReadDelivery(ctx, w.rt, store.DeliveryQuery{Commit: externalCommit}, nil, nil)
		ok(t, err)
		delivery.Cases = append(delivery.Cases, consoleFixtureCase{Name: name, Query: "commit=" + externalCommit, Response: encode(out)})
	}
	external("external work: CI green, waiting for STG")
	recordRuns(t, w, externalRun("bifrost-deliver-platform-0", "bifrost-deliver-platform", "Failed", started.Add(10*time.Minute), time.Minute))
	external("external work: STG delivery failed")
	recordRuns(t, w, externalRun("bifrost-deliver-platform-1", "bifrost-deliver-platform", "Succeeded", started.Add(15*time.Minute), 5*time.Minute),
		externalRun("bifrost-deliver-platform-prod-1", "bifrost-deliver-platform-prod", "Succeeded", started.Add(30*time.Minute), time.Minute))
	external("external work: on PROD by its recorded runs")

	var timeline consoleFixture
	for _, q := range []struct{ name, node string }{{"round", ""}, {"one step", s2}} {
		out, err := store.ReadRoundTimeline(ctx, w.rt, root, q.node)
		ok(t, err)
		query := "root=" + root
		if q.node != "" {
			query += "&node=" + q.node
		}
		timeline.Cases = append(timeline.Cases, consoleFixtureCase{Name: q.name, Query: query, Response: encode(out)})
	}

	for _, f := range []struct {
		file string
		got  consoleFixture
	}{{consoleDeliveryFile, delivery}, {consolePlanTimeline, timeline}} {
		path := filepath.Join(consoleFixtures, f.file)
		if os.Getenv("CONSOLE_DELIVERY_WRITE") != "" {
			raw, err := json.Marshal(f.got)
			ok(t, err)
			var out bytes.Buffer
			ok(t, json.Indent(&out, raw, "", " "))
			out.WriteByte('\n')
			ok(t, os.WriteFile(path, out.Bytes(), 0o644))
			t.Logf("wrote %s", f.file)
			continue
		}
		body, err := os.ReadFile(path)
		ok(t, err)
		var stored consoleFixture
		ok(t, json.Unmarshal(body, &stored))
		if want, got := consoleKeys(t, stored), consoleKeys(t, f.got); !slices.Equal(want, got) {
			t.Errorf("%s: the stored answers carry keys %v; the reads now carry %v (regenerate it, and bring the Console's parser along)", f.file, want, got)
		}
	}
}

// consoleRawLanding appends a controller landing note at at, chained as
// Append chains it.
func consoleRawLanding(tx pgx.Tx, stream string, at time.Time, record map[string]any) error {
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx, `SELECT last_seq,last_sha256 FROM agentrt.event_stream WHERE event_stream_id=$1 FOR UPDATE`, stream).Scan(&seq, &prev); err != nil {
		return err
	}
	at = at.UTC().Truncate(time.Microsecond)
	body := map[string]any{"text": "Landing protocol test", "landing": record}
	env := map[string]any{"v": 1, "stream": stream, "seq": seq + 1, "type": "note", "by": "controller",
		"at": at.Format("2006-01-02T15:04:05.000000Z"), "prev_sha256": prev, "mission_id": nil,
		"plan_node_id": nil, "task_id": nil, "attempt_id": nil, "cause_event_id": nil, "body": body}
	sum, err := canon.Sum(env)
	if err != nil {
		return err
	}
	raw, err := canon.Encode(body)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.event(event_stream_id,seq,event_type,actor_id,body_json,prev_sha256,sha256,at)
	 VALUES($1,$2,'note','controller',$3::jsonb,$4,$5,$6)`, stream, seq+1, string(raw), prev, sum, at)
	return err
}

// consoleKeys are the key paths of every case's answer, arrays as [].
func consoleKeys(t *testing.T, f consoleFixture) []string {
	t.Helper()
	seen := map[string]bool{}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				seen[path+"."+k] = true
				walk(path+"."+k, e)
			}
		case []any:
			for _, e := range x {
				walk(path+"[]", e)
			}
		}
	}
	for _, c := range f.Cases {
		var v any
		ok(t, json.Unmarshal(c.Response, &v))
		walk("", v)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, strings.TrimPrefix(k, "."))
	}
	sort.Strings(out)
	return out
}
