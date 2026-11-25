package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/schemawatch"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const migCommit = "1111111111111111111111111111111111111111"

// A migration that cannot destroy rows reaches PROD without a card; the
// platform runs it (cards #493 #554 #558 were Owner-run Jobs) (LAW-6).
// What the policy reads of this platform is what /health reports (the
// applied migrations by number and sha256; an unread schema is an error),
// here as the server wires it: no runtime, and a schema not read yet.
func TestOwnMigrationsAreWiredFromTheRuntime(t *testing.T) {
	s := &Server{}
	if _, err := (ownMigrations{s}).Own(context.Background()); err == nil {
		t.Fatal("a platform without the runtime answered")
	}
	s.agentRuntime = &agentRuntime{env: "prod", schema: &schemawatch.Watch{}}
	if _, err := (ownMigrations{s}).Own(context.Background()); err == nil || !strings.Contains(err.Error(), "have not been read") {
		t.Fatalf("an unread schema: %v", err)
	}
}

// A migration that cannot destroy rows reaches PROD without a card; the
// platform runs it (cards #493 #554 #558 were Owner-run Jobs) (LAW-6).
// The action is in the catalog at tier C with the platform as its runner, so
// an approved card is run by the platform and not from a terminal.
func TestMigrationActionIsTierCRunByThePlatform(t *testing.T) {
	act, ok := actions.ByID(migrationActionID)
	if !ok {
		t.Fatal("not in the catalog")
	}
	params := map[string]any{"commit": migCommit}
	if act.TierOf(context.Background(), params) != actions.TierC || act.RunnerOf(params) != actions.RunnerPlatform ||
		act.Method != "POST" || act.Pattern != "/api/v1/agentrt/migrations/run" {
		t.Fatalf("action = %+v", act.View())
	}
	if got := act.Describe(context.Background(), params, "").Summary; !strings.Contains(got, "pending database migrations of 1111111") {
		t.Fatalf("summary = %q", got)
	}
}

// migrationHarness is the policy harness on a platform that deliver-pinned
// delivers: it runs the old commit of repo-app, has applied 0001, and the
// source pipeline delivered the new commit, which adds 0002 with the text
// second. reversible signs the policy with ddl_reversible and its paths.
func migrationHarness(t *testing.T, reversible bool, second string) *policyHarness {
	t.Helper()
	h := newPolicyHarness(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if reversible {
		text := rptest.PolicyTextWith("rp-migrate", now.Add(-time.Hour), 90, func(doc map[string]any) {
			doc["conditions"].(map[string]any)["ddl_reversible"] = true
			doc["ddl_reversible_paths"] = []map[string]any{{"repo": "repo-app", "paths": []string{"db/migrations/*.sql"}}}
		}, policyPipeline, "deliver-pinned")
		h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
	}
	const first = "CREATE TABLE orders (id bigint);\n"
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	h.facts.Dirs = map[string][]string{"repo-app@" + newSHA + ":db/migrations": {"0001_core.sql", "0002_notes.sql"}}
	h.facts.Text = map[string]string{
		"repo-app@" + oldSHA + ":db/migrations/0001_core.sql":  first,
		"repo-app@" + newSHA + ":db/migrations/0001_core.sql":  first,
		"repo-app@" + newSHA + ":db/migrations/0002_notes.sql": second,
	}
	h.facts.Records["deliver-pinned"] = map[string]string{"repo-app": oldSHA}
	h.facts.Records["deliver-app-stg"] = map[string]string{"repo-app": newSHA}
	sum := sha256.Sum256([]byte(first))
	h.srv.releasePolicy = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: h.cms, Writer: rptest.Writer{CMs: h.cms}, Git: h.facts, CI: h.facts, Window: h.facts, Deployed: h.facts,
		Migrations: &rptest.Migrations{State: releasepolicy.OwnMigrations{
			Pipeline: "deliver-pinned", Repo: "repo-app", Dir: "db/migrations", Applied: map[int]string{1: hex.EncodeToString(sum[:])},
		}},
		Anchor: h.key.Fingerprint, Now: func() time.Time { return now },
	})
	h.srv.approvals.SetAutoApprover(h.srv.releasePolicyApprover())
	return h
}

func (h *policyHarness) directMigration(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agentrt/migrations/run", strings.NewReader(`{"commit":"`+rptest.SHA("new")+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.guard(migrationActionID, func(w http.ResponseWriter, r *http.Request) {
		h.hits++
		w.WriteHeader(http.StatusOK)
	})(rec, req)
	return rec
}

func (h *policyHarness) createMigration(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(
		`{"action":"run_agentrt_migration","params":{"commit":"`+rptest.SHA("new")+`"},"reason":"migrate"}`))
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.approvals.HandleCreate(rec, req)
	return rec
}

// A migration that cannot destroy rows reaches PROD without a card; the
// platform runs it (cards #493 #554 #558 were Owner-run Jobs) (LAW-6).
// The direct call the signed policy covers reaches the handler and is
// audited with the files it let through; the approval route answers "call
// directly". The same call with a DROP TABLE in the pending file is 403 with
// the file and the statement kind, and a card can be created for it.
func TestPolicyCoversADirectMigrationAndAudits(t *testing.T) {
	h := migrationHarness(t, true, "CREATE TABLE notes (id bigint);\n")
	rec := h.directMigration(t)
	if rec.Code != http.StatusOK || h.hits != 1 {
		t.Fatalf("direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	want := "auto-approved policy_id=rp-migrate requester=sess-rp sha=repo-app=" + rptest.SHA("new") +
		" clauses=signature,unexpired,not_frozen,allow:deliver-pinned,pinned_to:deliver-app-stg,ddl_reversible ddl_reversible=repo-app:db/migrations/0002_notes.sql"
	if a := h.auditText(t); !strings.Contains(a, want) || !strings.Contains(a, "run_agentrt_migration "+rptest.SHA("new")) {
		t.Fatalf("audit lacks %q:\n%s", want, a)
	}
	c := h.createMigration(t)
	var body map[string]any
	_ = json.Unmarshal(c.Body.Bytes(), &body)
	if c.Code != http.StatusBadRequest || body["error"] != "call directly" || body["auto_approved_by"] != "rp-migrate" {
		t.Fatalf("create = %d %s", c.Code, c.Body.String())
	}

	h = migrationHarness(t, true, "DROP TABLE orders;\n")
	rec = h.directMigration(t)
	if rec.Code != http.StatusForbidden || h.hits != 0 || !strings.Contains(rec.Body.String(), "db/migrations/0002_notes.sql: statement 1: DROP TABLE is not a statement of the class") {
		t.Fatalf("a DROP TABLE: direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	if c := h.createMigration(t); c.Code != http.StatusCreated {
		t.Fatalf("a DROP TABLE: create = %d %s", c.Code, c.Body.String())
	}
	if a := h.auditText(t); strings.Contains(a, "release_policy.auto_approve") {
		t.Fatalf("a refused migration was audited as approved:\n%s", a)
	}
}

// A migration that cannot destroy rows reaches PROD without a card; the
// platform runs it (cards #493 #554 #558 were Owner-run Jobs) (LAW-6).
// Under a policy signed before ddl_reversible existed the action always
// needs a card: the direct call is the plain 403 of any tier C action, with
// no release_policy reasons and nothing audited.
func TestAPolicyWithoutDDLReversibleAlwaysNeedsACardForAMigration(t *testing.T) {
	h := migrationHarness(t, false, "CREATE TABLE notes (id bigint);\n")
	rec := h.directMigration(t)
	if got, want := strings.TrimSpace(rec.Body.String()), `{"action":"run_agentrt_migration","error":"approval required"}`; rec.Code != http.StatusForbidden || h.hits != 0 || got != want {
		t.Fatalf("direct = %d hits=%d body = %s, want %s", rec.Code, h.hits, got, want)
	}
	if c := h.createMigration(t); c.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", c.Code, c.Body.String())
	}
	if a := h.auditText(t); strings.Contains(a, "release_policy") {
		t.Fatalf("the policy was audited for a migration it does not judge:\n%s", a)
	}
}

// LAW-6: the migration action's errors are the platform's classes
// (an uncertain Job outcome is a 504, never a retry) and its Job names the
// approval that ran it; migraterun is handed them, and this is where they are
// the actions package's.
func TestMigrationErrorsAreTheActionsClasses(t *testing.T) {
	if !actions.IsUncertain(migrationErrors.Uncertain("the Job may still apply")) || !actions.IsTransient(migrationErrors.Transient("the cluster is busy")) {
		t.Fatal("the classes are not the actions package's")
	}
	ctx := actions.WithParamsHash(actions.WithCreateAttempt(context.Background(), "apr_1", 1), "hash")
	if got := migrationErrors.Identity(ctx)[actions.AnnApprovalID]; got != "apr_1" {
		t.Fatalf("the Job names approval %q", got)
	}
}
