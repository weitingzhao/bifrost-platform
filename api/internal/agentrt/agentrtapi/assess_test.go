package agentrtapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of two assessment streams
// (console/src/api/__fixtures__/assessment-streams.testdb.json): GET
// /api/v1/agentrt/events?stream=assess:… as the Owner's Console reads it,
// for a card assessed and an approval whose assessment is unavailable. Off
// unless asked for:
//
//	CONSOLE_ASSESSMENT_FIXTURE_WRITE=1 go test ./internal/agentrt/agentrtapi/ -run TestAssessmentThroughTheRoutes
const consoleAssessmentFixture = "../../../../console/src/api/__fixtures__/assessment-streams.testdb.json"

// TD-391 through the routes agentd and the Console use: the controller gives
// a card's and an approval's assessments to the chief of staff's seat; an
// agentd host claims each as the seat's executor, hands in its report and
// submits; the controller records the card's assessment and the approval's
// unavailability (its report is not one); the Console reads both streams.
// No route writes to them: there is no POST /events.
func TestAssessmentThroughTheRoutes(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the chief of staff's job"}}); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('chief_of_staff', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
			 VALUES ('chief_of_staff/exec:codex/openai', 'chief_of_staff', 'exec:codex', 'openai', 'active', 'job:test', 1)`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	const card = "q_0123456789abcdef"
	sha := "6b9507328434ff6cdb47ad46e48ae851b9c8d5ae"
	ev := store.Evidence{Org: "bifrost", LedgerRepo: "bifrost-trade-infra", LedgerPath: "agent-config/TECH_DEBT.md",
		Commits: []store.EvidenceCommit{{Repo: "bifrost-platform", SHA: sha, Subject: "agentrt: the infra check runs scripts/test_access_checks.py (TD-378)",
			At: time.Now().Add(-time.Hour), Work: []string{"TD-378"}}},
		Ledger: []store.LedgerItem{{ID: "TD-378", Title: "P2 · agentrt · the infra check leaves a file out", State: "状态：待你签收"}}}
	items := []store.AssessItem{
		{Kind: store.AssessCard, ID: card, Title: "Blocked in M-1: t", Body: "Recommended: Give it one more round — only the budget stops it",
			Options: []store.AssessOption{{Key: "a", Label: store.BlockedRetry, Detail: "only the budget stops it"}, {Key: "b", Label: store.BlockedDrop},
				{Key: "c", Label: "Close mission M-1"}}, Recommended: "a", TaskID: task, CreatedAt: time.Now()},
		{Kind: store.AssessApproval, ID: "12", Title: "Deliver platform to PROD", Body: "STG runs 8f30a9f6", Options: store.ApprovalOptions(),
			CreatedAt: time.Now(), Record: map[string]any{"action": "deliver_platform", "tier": "C"}},
	}
	reports := []string{"```json\n" + `{"recommended": "b", "confidence": "high", "reasons": [` +
		`{"text": "这项工作已经在 main 上：6b950732。", "evidence": ["commit:bifrost-platform@6b9507328434"]},` +
		`{"text": "台账 TD-378 等签收，防线就是这个提交。", "evidence": ["ledger:TD-378", "commit:bifrost-platform@6b9507328434"]}],` +
		`"unchecked": ["STG 是否已经跑着这个提交"]}` + "\n```\n", "I would approve it."}
	codex := map[string]any{"executor": "exec:codex", "provider": "openai"}
	for i, item := range items {
		must(t, store.RequestAssessment(ctx, pool, store.Writer{Actor: "owner"}, item.Kind, item.ID, func(context.Context) error { return nil }))
		id, err := store.GiveAssessment(ctx, pool, item, ev, nil, false)
		must(t, err)
		var mission string
		must(t, pool.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, id).Scan(&mission))
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			var state string
			if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&state); err != nil || state == "active" {
				return err
			}
			return store.MoveMission(ctx, tx, "controller", mission, "active", "test")
		}))
		_, err = store.Dispatch(ctx, pool)
		must(t, err)
		status, claim := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{codex}})
		if status != http.StatusOK || claim["task_id"] != id || claim["role"] != "brief" {
			t.Fatalf("claim: %d %v", status, claim)
		}
		attempt := claim["attempt_id"].(string)
		fence := func(extra map[string]any) map[string]any {
			b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
			for k, v := range extra {
				b[k] = v
			}
			return b
		}
		report := []byte(reports[i])
		if code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report",
			"Content-Type", "text/markdown"); code != http.StatusOK {
			t.Fatalf("upload: %d %v", code, out)
		}
		for _, step := range []struct {
			action string
			body   map[string]any
		}{
			{"start", fence(map[string]any{"model_provider": "openai"})},
			{"computed", fence(map[string]any{"exit_code": 0, "end_how": "exit", "conclusion": "done"})},
			{"persist", fence(map[string]any{"report": artifactID(report)})},
			{"submit", fence(nil)},
		} {
			if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
				t.Fatalf("%s: %d %v", step.action, code, out)
			}
		}
		want := []string{"assessed", "unavailable"}[i]
		if did, err := store.AcceptBrief(ctx, pool, id); err != nil || did != want {
			t.Fatalf("accept %s: %q %v", item.ID, did, err)
		}
	}

	streams := map[string]json.RawMessage{}
	for key, stream := range map[string]string{"card": store.AssessStream(store.AssessCard, card), "approval": store.AssessStream(store.AssessApproval, "12")} {
		raw := get(t, s, "events?stream="+stream)
		var rows []map[string]any
		must(t, json.Unmarshal(raw, &rows))
		if len(rows) != 3 || rows[0]["event_type"] != "note" {
			t.Fatalf("%s: %s", stream, raw)
		}
		last := rows[2]["body_json"].(map[string]any)
		switch key {
		case "card":
			a := last["assessment"].(map[string]any)
			ref := a["reasons"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
			if a["recommended"] != "b" || a["by"] != "exec:codex" || ref["sha"] != sha || ref["org"] != "bifrost" {
				t.Fatalf("the card's assessment: %v", a)
			}
		case "approval":
			if last["unavailable"] != "its report is not an assessment: no JSON object" {
				t.Fatalf("the approval's: %v", last)
			}
		}
		streams[key] = raw
	}
	if code, _ := call(t, s, "agentd:h1", "POST", "events", map[string]any{"stream": "assess:card:" + card}); code != http.StatusMethodNotAllowed &&
		code != http.StatusNotFound {
		t.Fatalf("a note was taken by a route: %d", code)
	}
	if os.Getenv("CONSOLE_ASSESSMENT_FIXTURE_WRITE") == "1" {
		raw, err := json.MarshalIndent(streams, "", "  ")
		must(t, err)
		must(t, os.WriteFile(consoleAssessmentFixture, append(raw, '\n'), 0o644))
	}
	if strings.Contains(string(streams["card"]), "/Users/") {
		t.Fatal("a local path in the stream")
	}
}
