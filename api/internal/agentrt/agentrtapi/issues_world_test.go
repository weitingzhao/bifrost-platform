package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Business issues over HTTP (W-31 BI-1 slice 2): the fixture every issue
// test and the routes' T25 world share. Every text is synthetic; no
// person's real words go into a fixture (DESIGN-crew-intake §3.1).

// The intake client's token comes from its environment variable only
// (intake_tokens:, §5.2), set for the whole test binary.
const (
	fixtureIntakeEnv   = "AGENTRTAPI_FIXTURE_INTAKE_TOKEN"
	fixtureIntakeToken = "fixture-intake-1"
)

func init() {
	if err := os.Setenv(fixtureIntakeEnv, fixtureIntakeToken); err != nil {
		panic(err)
	}
}

const intakeGatesRule = `rule_id: intake-gates
rule_version: 1
direction: initial
effective_at: 2026-01-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
classes:
  small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]
  medium: [brief_confirmed, classified, owner_priority, design, review, quota, rule_in_force]
  large: [brief_confirmed, classified, owner_priority, design, review, owner_decisions, quota, rule_in_force]
max_live_per_product: 2
exceptions: [answer_only, fault_small]
`

// issueFixture sets an issue world up on the API's world: the intake-gates
// rule in force, the Owner and p2, the intake client agent:dots-p2 for p2
// (screening), and a seat's report to cite (the report of an accepted brief
// task of M-1). It returns the report's id.
func issueFixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	putRuleText(t, pool, intakeGatesRule)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	for id, name := range map[string]string{store.OwnerPerson: "Owner", "person:p2": "Person two"} {
		_, err := store.PutPerson(ctx, pool, owner, id, name)
		must(t, err)
	}
	_, err := store.SetIntakeClient(ctx, pool, owner, store.IntakeClientSetup{ClientID: "agent:dots-p2", OnBehalfOf: "person:p2",
		Products: []string{"screening"}})
	must(t, err)
	var report string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: a seat's brief"}}); err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# seat brief "+store.NewTaskID()))
		if err != nil {
			return err
		}
		report, err = store.PutArtifact(ctx, tx, "controller", "report", "text/markdown", []byte("seat answer "+store.NewTaskID()))
		if err != nil {
			return err
		}
		task := store.NewTaskID()
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, off_plan, off_plan_reason, role, task_class, title,
		    spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id)
		  VALUES ($1, 'M-1', true, 'a seat brief', 'brief', 'brief', 'seat brief', $2, 'none', 'accepted', 60, 'controller')`, task, spec); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch, lease_expires_at,
		    heartbeat_at, state, report_artifact_id) VALUES ($1, $2, 1, 'exec:claude', 1, now(), now(), 'finished', $3)`,
			store.NewAttemptID(), task, report)
		return err
	}))
	return report
}

// vnum is a number of an answer, read over HTTP (float64) or from the store
// (json.Number, int).
func vnum(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case int:
		return n
	case int64:
		return int(n)
	}
	panic(fmt.Sprintf("%T %v is no number", v, v))
}

// putRuleText stores a rule as if the Owner had signed it.
func putRuleText(t *testing.T, pool *pgxpool.Pool, text string) {
	t.Helper()
	r, err := rules.Parse([]byte(text))
	must(t, err)
	body, err := r.BodyJSON()
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ($1, $2, $3::jsonb, $4, $5, 'test', 'test', $6, $7)`, r.ID, r.Version, string(body), text, r.Direction,
		r.Effective, r.Expires)
	must(t, err)
}

// validIssueRoutes sends every issue route its valid request (T25): two
// issues of p2 and the Owner worked from bringing to taking, a design, its
// review, a park and a resume, a close and a reopen, a merge, an intake
// client's reads.
func (f *stateFuzz) validIssueRoutes(t *testing.T, ok func(key, caller, method, path string, body any, header ...string) map[string]any,
	seat string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ver := func(out map[string]any) int { return vnum(out["version"]) }
	words := []any{map[string]any{"text": "the page shows nothing", "said_at": now, "where": "the screen page"}}
	out := ok("POST intake/issues", "intake:dots-p2", "POST", "intake/issues", map[string]any{"idempotency_key": "i-1", "product": "screening",
		"kind": "complaint", "on_behalf_of": "person:p2", "title": "empty page", "person_words": words, "agent_reading": "it reads empty"})
	a := out["issue_id"].(string)
	out = ok("POST intake/issues/{id}/additions", "intake:dots-p2", "POST", "intake/issues/"+a+"/additions",
		map[string]any{"idempotency_key": "i-2", "agent_reading": "and again"})
	file := func(key, title string) map[string]any {
		return ok("POST issues", "operator", "POST", "issues", map[string]any{"idempotency_key": key, "product": "screening", "kind": "request",
			"on_behalf_of": store.OwnerPerson, "title": title, "person_words": []any{map[string]any{"text": "owner words " + title}}, "channel": "thread"})
	}
	b := file("c-1", "owner's own")["issue_id"].(string)
	ok("POST issues/{id}/additions", "operator", "POST", "issues/"+b+"/additions",
		map[string]any{"idempotency_key": "c-2", "person_words": []any{map[string]any{"text": "more owner words"}}, "channel": "thread"})

	out = ok("POST issues/{id}/brief", "operator", "POST", "issues/"+a+"/brief", map[string]any{"idempotency_key": "c-3",
		"expected_version": ver(out), "text": "brief of a"})
	askA := out["ask_id"]
	out = ok("POST intake/issues/{id}/responses", "intake:dots-p2", "POST", "intake/issues/"+a+"/responses", map[string]any{"idempotency_key": "i-3",
		"expected_version": ver(out), "in_reply_to": askA, "user_words": "yes, that", "said_at": now, "user_verdict": "confirmed"})
	out = ok("POST issues/{id}/classification", "operator", "POST", "issues/"+a+"/classification", map[string]any{"idempotency_key": "c-4",
		"expected_version": ver(out), "risk_class": "small", "category": "improvement", "seat_report": seat})
	out = ok("POST issues/{id}/owner-cards", "operator", "POST", "issues/"+a+"/owner-cards", map[string]any{"idempotency_key": "c-5",
		"expected_version": ver(out), "purpose": "priority"})
	out, err := store.RecordOwnerEvidence(ctx, f.pool, store.Writer{Actor: "owner", Token: "admin"}, "o-1", store.OwnerEvidence{IssueID: a,
		Priority: "p2", BriefSHA256: fmt.Sprint(out["brief_sha256"])})
	must(t, err)
	out = ok("POST issues/{id}/take", "operator", "POST", "issues/"+a+"/take", map[string]any{"idempotency_key": "c-6",
		"expected_version": ver(out), "commitment": "show the page's rows",
		"mission": map[string]any{"title": "fix the page", "goal": "rows show", "success": []string{"rows show"},
			"budget": map[string]any{"max_attempts": 4, "max_wall_hours": 2}}})
	out = ok("POST issues/{id}/replies", "operator", "POST", "issues/"+a+"/replies", map[string]any{"idempotency_key": "c-7",
		"expected_version": ver(out), "purpose": "reply", "text": "we are on it"})
	ok("POST issues/{id}/close-checks", "operator", "POST", "issues/"+a+"/close-checks", map[string]any{"idempotency_key": "c-8",
		"expected_version": ver(out), "ask_id": askA, "result": "supports", "seat_report": seat})

	out = ok("POST issues/{id}/brief", "operator", "POST", "issues/"+b+"/brief", map[string]any{"idempotency_key": "c-9",
		"expected_version": 1, "text": "brief of b"})
	out = ok("POST issues/{id}/responses", "admin", "POST", "issues/"+b+"/responses", map[string]any{"idempotency_key": "c-10",
		"expected_version": ver(out), "in_reply_to": out["ask_id"], "user_words": "yes", "user_verdict": "confirmed", "channel": "console"})
	out = ok("POST issues/{id}/design", "operator", "POST", "issues/"+b+"/design", map[string]any{"idempotency_key": "c-11",
		"expected_version": ver(out), "text": "design of b"})
	out = ok("POST issues/{id}/reviews", "operator", "POST", "issues/"+b+"/reviews", map[string]any{"idempotency_key": "c-12",
		"expected_version": ver(out), "report": "looked at it\n通过", "reviewer_tool": "codex", "author_tool": "claude", "round": 1})
	out = ok("POST issues/{id}/park", "operator", "POST", "issues/"+b+"/park", map[string]any{"idempotency_key": "c-13",
		"expected_version": ver(out), "reason": "later"})
	out = ok("POST issues/{id}/resume", "operator", "POST", "issues/"+b+"/resume", map[string]any{"idempotency_key": "c-14",
		"expected_version": ver(out)})
	out = ok("POST issues/{id}/close", "admin", "POST", "issues/"+b+"/close", map[string]any{"idempotency_key": "c-15",
		"expected_version": ver(out), "conclusion": "declined", "reason": "not now"})
	ok("POST issues/{id}/reopen", "operator", "POST", "issues/"+b+"/reopen", map[string]any{"idempotency_key": "c-16",
		"expected_version": ver(out)})

	c, d := file("c-17", "twice"), file("c-18", "twice again")
	ok("POST issues/merge", "operator", "POST", "issues/merge", map[string]any{"idempotency_key": "c-19", "from": c["issue_id"],
		"into": d["issue_id"], "from_version": ver(c), "into_version": ver(d)})
	ok("POST intake/updates", "intake:dots-p2", "POST", "intake/updates", map[string]any{"limit": 5})
}
