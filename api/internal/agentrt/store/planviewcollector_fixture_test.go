package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The plan view of a collector's external work (R5:5.5-12 P-c1, §7 mixed
// versions): the actual PlanView builder over the signoff fixture's
// deterministic rows plus external work notes in every place a row goes: a
// collector's running work (external_work, working), one it submitted today
// (finished_today), one linked to a task whose attempt is live (that task's
// history, in tasks and in working), each with its host and session
// identity (P-b), and an MCP registrant's (none). The answer is the
// Console's fixture byte for byte, so the Console's strict reader is tested
// against what Go writes.
// Update explicitly: UPDATE_PLAN_COLLECTOR_FIXTURE=1 go test ./internal/agentrt/store -run TestPlanViewCollectorWorkFixture
const planCollectorFixture = "../../../../console/src/api/__fixtures__/plan-view-collector.generated.json"

type collectorWorkViewDB struct {
	*signoffViewDB
	notes [][]any
	descs map[string][]byte
	tasks []byte
	live  [][]any
	waits [][]any
}

// The task whose attempt is live, its attempt, and the collector work linked to it.
const (
	fixtureLiveTask    = "t_fixture_live"
	fixtureLiveAttempt = "a_fixture_live"
)

func (d *collectorWorkViewDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if _, err := d.signoffViewDB.BeginTx(ctx, opts); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *collectorWorkViewDB) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if strings.Contains(q, "jsonb_build_object('task_id',t.task_id") {
		return collectorTestRow(func(dest ...any) error { *dest[0].(*[]byte) = d.tasks; return nil })
	}
	if strings.Contains(q, "a.artifact_kind='other'") {
		return collectorTestRow(func(dest ...any) error {
			body, ok := d.descs[args[0].(string)]
			if !ok {
				return pgx.ErrNoRows
			}
			*dest[0].(*[]byte) = body
			return nil
		})
	}
	return d.signoffViewDB.QueryRow(ctx, q, args...)
}

func (d *collectorWorkViewDB) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(q, "SELECT body,at FROM") {
		return &signoffViewRows{data: d.notes}, nil
	}
	if strings.HasPrefix(q, "SELECT a.attempt_id") {
		return &signoffViewRows{data: d.live}, nil
	}
	if strings.Contains(q, planViewWaitFacts) {
		return &signoffViewRows{data: d.waits}, nil
	}
	return d.signoffViewDB.Query(ctx, q, args...)
}

func newCollectorWorkViewFixture(t *testing.T) *collectorWorkViewDB {
	t.Helper()
	f := &collectorWorkViewDB{signoffViewDB: newSignoffViewFixture(t), descs: map[string][]byte{}}
	at := f.now.Add(-10 * time.Second)
	digest := "sha256:" + strings.Repeat("d", 64)
	collector := func(id, session string, epoch int64, node string) externalNote {
		return externalNote{ID: "ew_" + strings.Repeat(id, 64), Actor: CollectorActorPrefix + "h1", Host: "h1", Vendor: "claude",
			Session: session, Epoch: epoch, Node: node, RunKey: IdentityRunKey("claude", session, epoch)}
	}
	running := collector("1", "0b8e2f4a-1111-2222-3333-444455556666", 1, "R4:4.1-1")
	running.Sidebar, running.Seq, running.Type, running.State = "local_0b8e2f4a-aaaa-bbbb-cccc-ddddeeeeffff", 1, "started", "Running"
	submitted := collector("3", "7c1d9e3b-1111-2222-3333-444455556666", 2, "R4:4.2-1")
	submitted.Seq, submitted.Type, submitted.State, submitted.EndedAt = 2, "submitted", "Submitted", timeText(f.now.Add(-5*time.Second))
	linked := collector("4", "9e5f0a1c-1111-2222-3333-444455556666", 1, "R4:4.3-1")
	linked.Mission, linked.Task, linked.Seq, linked.Type, linked.State = "M-fixture", fixtureLiveTask, 1, "started", "Running"
	for i, n := range []externalNote{
		running,
		{ID: "ew_" + strings.Repeat("2", 64), Actor: "thread:local_mcp", Node: "R4:4.2-1", RunKey: "mcp-run", Type: "registered", State: "Registered"},
		submitted,
		linked,
	} {
		task := []string{"collector hook work", "an MCP registration", "collector work submitted today", "collector work beside a live attempt"}[i]
		desc, _ := json.Marshal(ExternalDescription{V: 1, Kind: "agentrt.external_work/v1", Task: task, Executor: "claude", Provider: "anthropic",
			Repo: "platform", Branch: "test", Acceptance: []string{"PV1"}, Estimate: []int{1, 2}})
		n.V, n.Kind, n.Root, n.Artifact, n.RequestDigest, n.RegistrationDigest = 1, "agentrt.external_work/v1", "R4", "sha256:"+strings.Repeat(string(rune('a'+i)), 64), digest, digest
		n.RegisteredAt, n.HeartbeatAt = timeText(at), timeText(at)
		if n.State == "Running" {
			n.StartedAt = timeText(at)
		}
		f.descs[n.Artifact] = desc
		raw, _ := json.Marshal(n)
		f.notes = append(f.notes, []any{raw, at})
	}
	f.tasks, _ = json.Marshal([]map[string]any{{"task_id": fixtureLiveTask, "plan_node_id": "R4:4.3-1", "mission_id": "M-fixture", "title": "a task with a live attempt", "state": "running",
		"role": "implement", "round_no": 1, "max_rounds": 3, "depends_on": []string{}}})
	// It came into running when its attempt started; its mission is active.
	since := at
	f.waits = [][]any{{fixtureLiveTask, &since, "", "active", "", (*time.Time)(nil), (*string)(nil), (*time.Time)(nil), "", []byte(`[]`)}}
	started := at
	f.live = [][]any{{fixtureLiveAttempt, 1, fixtureLiveTask, "M-fixture", "h1", true, &at, (*bool)(nil), "exec:claude", "claude-opus", "running",
		&started, at, "implement", "go", "running", false, (*int64)(nil), (*time.Time)(nil)}}
	return f
}

func TestPlanViewCollectorWorkFixture(t *testing.T) {
	t.Parallel()
	f := newCollectorWorkViewFixture(t)
	out, err := PlanView(context.Background(), f, f.doc.Root)
	if err != nil {
		t.Fatal(err)
	}
	ext, _ := out["external_work"].([]map[string]any)
	working, finished := out["working"].([]any), out["finished_today"].([]any)
	tasks, _ := out["tasks"].([]map[string]any)
	if len(ext) != 4 || len(working) != 3 || len(finished) != 1 || len(tasks) != 1 {
		t.Fatalf("external work %d, working %d, finished %d, tasks %d", len(ext), len(working), len(finished), len(tasks))
	}
	// Every place a row goes: a collector's names its host and identity, the
	// MCP registrant's none of it.
	rows := map[string]map[string]any{"finished_today": finished[0].(map[string]any)}
	for i, e := range ext {
		rows[fmt.Sprintf("external_work[%d]", i)] = e
	}
	for _, w := range working {
		if m := w.(map[string]any); m["external_work_id"] != nil {
			rows["working/"+m["external_work_id"].(string)] = m
		} else if history, _ := m["external_work"].([]map[string]any); len(history) == 1 {
			rows["working task history"] = history[0]
		}
	}
	if history, _ := tasks[0]["external_work"].([]map[string]any); len(history) == 1 {
		rows["task history"] = history[0]
	}
	if len(rows) != 9 {
		t.Fatalf("the rows of every place: %d", len(rows))
	}
	for where, row := range rows {
		mcp := row["registrant"] == "thread:local_mcp"
		for _, k := range []string{"host", "vendor", "session", "epoch"} {
			if _, ok := row[k]; ok == mcp {
				t.Fatalf("%s (%s) and %s: %v", where, row["registrant"], k, row)
			}
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if os.Getenv("UPDATE_PLAN_COLLECTOR_FIXTURE") == "1" {
		if err = os.WriteFile(planCollectorFixture, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(planCollectorFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("Go plan-view response drifted; regenerate and review the Console fixture")
	}
}
