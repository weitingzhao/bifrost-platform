package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

const (
	migCommit = "1111111111111111111111111111111111111111"
	migHead   = "2222222222222222222222222222222222222222"
	migDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	migNS     = "platform-prod-ns"
	// The shape of bifrost-trade-infra k8s/agentrt-migrate/prod/job.yaml.
	migManifest = `# Owner-created migration Job
apiVersion: batch/v1
kind: Job
metadata:
  generateName: agentrt-migrate-
  namespace: platform-prod-ns
  labels:
    app.kubernetes.io/name: agentrt-migrate
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 900
  template:
    spec:
      restartPolicy: Never
      initContainers:
        - name: wait-pg
          image: example.org/postgresql:17.9
      containers:
        - name: migrate
          image: registry.example:30500/the-api:prod
          command: [/usr/local/bin/platform-api]
          args: [agentrt-migrate]
          env:
            - name: AGENTRT_ENV
              value: prod
`
)

// migrationFixture is the runner on a fake cluster whose Job finishes as
// finish says, with one pending migration and a successful source delivery.
type migrationFixture struct {
	m       *migrationRunner
	cs      *fake.Clientset
	state   releasepolicy.MigrationState
	records []releases.Record
	finish  batchv1.JobConditionType // "" leaves the Job running
	log     string
	created []*batchv1.Job
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	f := &migrationFixture{
		cs: fake.NewSimpleClientset(),
		state: releasepolicy.MigrationState{
			Own:     releasepolicy.OwnMigrations{Pipeline: "deliver-prod", Repo: "the-repo", Dir: "migrations", Applied: map[int]string{1: "x"}},
			Pending: []string{"migrations/0002_notes.sql"},
		},
		records: []releases.Record{
			{Run: "stg-newer", Pipeline: "deliver-stg", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: "3333333333333333333333333333333333333333"}}},
			{Run: "stg-run", Pipeline: "deliver-stg", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: migCommit}}, Images: map[string]string{"platform-api": migDigest}},
			{Run: "prod-run", Pipeline: "deliver-prod", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: migCommit}}, Images: map[string]string{"platform-api": "sha256:" + strings.Repeat("b", 64)}},
		},
		finish: batchv1.JobComplete,
		log:    "ready\nagentrt-migrate: env=prod database=bifrost_platform applied=[2] seeded=false\n",
	}
	// The fake keeps generateName as it is; name the Job as the API server
	// would, and finish it as the fixture says.
	f.cs.PrependReactor("create", "jobs", func(a k8stesting.Action) (bool, runtime.Object, error) {
		job := a.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		f.created = append(f.created, job.DeepCopy())
		job.Name = job.GenerateName + "abcde"
		if f.finish != "" {
			job.Status.Conditions = []batchv1.JobCondition{{Type: f.finish, Status: corev1.ConditionTrue}}
		}
		return false, nil, nil
	})
	if _, err := f.cs.CoreV1().Pods(migNS).Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "agentrt-migrate-abcde-x1", Namespace: migNS, Labels: map[string]string{"job-name": "agentrt-migrate-abcde"},
	}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.m = &migrationRunner{
		env: "prod", namespace: migNS,
		state: func(context.Context, string) (releasepolicy.MigrationState, error) { return f.state, nil },
		source: func(_ context.Context, pipeline string) (string, bool) {
			return "deliver-stg", pipeline == "deliver-prod"
		},
		records:  func(context.Context) ([]releases.Record, error) { return f.records, nil },
		mainHead: func(context.Context, string) (string, error) { return migHead, nil },
		fileAt: func(_ context.Context, repo, ref, path string) (string, bool, error) {
			if repo != agentrtMigrateRepo || ref != migHead || path != "k8s/agentrt-migrate/prod/job.yaml" {
				return "", false, nil
			}
			return migManifest, true, nil
		},
		kube: func() (kubernetes.Interface, error) { return f.cs, nil },
		podLog: func(_ context.Context, _ kubernetes.Interface, namespace, pod string) (string, error) {
			if namespace != migNS || pod != "agentrt-migrate-abcde-x1" {
				return "", errors.New("no such pod")
			}
			return f.log, nil
		},
		wait: 200 * time.Millisecond, poll: 10 * time.Millisecond,
	}
	return f
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// The Job created is the environment's manifest with one change, the image:
// the api build the source environment's successful delivery of the commit
// verified, by digest; and the answer names the Job and what it applied.
func TestMigrationRunnerCreatesTheJobAndReadsWhatItApplied(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := actions.WithParamsHash(actions.WithCreateAttempt(context.Background(), "apr_1", 1), "hash")
	out, err := f.m.run(ctx, migCommit)
	if err != nil {
		t.Fatal(err)
	}
	if out["job"] != "agentrt-migrate-abcde" || out["namespace"] != migNS || out["already_current"] != false ||
		!slices.Equal(out["applied"].([]int), []int{2}) || out["image"] != "registry.example:30500/the-api@"+migDigest {
		t.Fatalf("answer = %v", out)
	}
	if len(f.created) != 1 {
		t.Fatalf("created %d jobs", len(f.created))
	}
	job := f.created[0]
	c := job.Spec.Template.Spec.Containers
	if job.GenerateName != "agentrt-migrate-" || job.Name != "" || job.Namespace != migNS || len(c) != 1 ||
		c[0].Image != "registry.example:30500/the-api@"+migDigest || !slices.Equal(c[0].Args, []string{"agentrt-migrate"}) ||
		job.Spec.Template.Spec.InitContainers[0].Image != "example.org/postgresql:17.9" || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("job = %+v", job)
	}
	if job.Annotations[annMigrateCommit] != migCommit || job.Annotations[actions.AnnApprovalID] != "apr_1" {
		t.Fatalf("annotations = %v", job.Annotations)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// A database that is current for the commit gets no Job, and the answer says so.
func TestMigrationRunnerDoesNothingWhenCurrent(t *testing.T) {
	f := newMigrationFixture(t)
	f.state.Pending = nil
	out, err := f.m.run(context.Background(), migCommit)
	if err != nil || out["already_current"] != true || out["job"] != "" || len(f.created) != 0 {
		t.Fatalf("answer = %v, %v; created %d", out, err, len(f.created))
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// Each case is the run above with one thing wrong. A refusal before the Job
// creates nothing; a Job that fails is an error with its log; a Job whose
// end was not seen is unknown, never retried under another name.
func TestMigrationRunnerRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		edit      func(f *migrationFixture)
		commit    string
		want      string
		created   int
		uncertain bool
	}{
		"not a commit id": {commit: "main", want: "40-character"},
		"the comparison fails": {edit: func(f *migrationFixture) {
			f.m.state = func(context.Context, string) (releasepolicy.MigrationState, error) {
				return releasepolicy.MigrationState{}, errors.New("0001_core.sql ran on this environment with other bytes")
			}
		}, want: "other bytes"},
		"the environment is ahead of the commit": {edit: func(f *migrationFixture) { f.state.Extra = []int{9} },
			want: "has applied migrations [9]"},
		"no policy pins the pipeline": {edit: func(f *migrationFixture) {
			f.m.source = func(context.Context, string) (string, bool) { return "", false }
		}, want: "no signed release policy pins deliver-prod"},
		"the commit was never delivered to the source": {edit: func(f *migrationFixture) { f.records = f.records[:1] },
			want: "no successful delivery of 111111111111 by deliver-stg"},
		"only this environment's own pipeline built the commit": {edit: func(f *migrationFixture) { f.records = f.records[2:] },
			want: "no successful delivery"},
		"the source record names no api digest": {edit: func(f *migrationFixture) { f.records[1].Images = map[string]string{"platform-console": migDigest} },
			want: "names no platform-api image digest"},
		"no manifest directory for this environment": {edit: func(f *migrationFixture) { f.m.env = "dev" },
			want: "has no k8s/agentrt-migrate/dev/job.yaml at main"},
		"the manifest is for another namespace": {edit: func(f *migrationFixture) { f.m.namespace = "platform-stg-ns" },
			want: `is for namespace "platform-prod-ns" and this platform runs in "platform-stg-ns"`},
		"the platform does not know its namespace": {edit: func(f *migrationFixture) { f.m.namespace = "" },
			want: "this platform runs in"},
		"the manifest is for another environment": {edit: func(f *migrationFixture) {
			f.m.fileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "value: prod", "value: stg", 1), true, nil
			}
		}, want: "does not set AGENTRT_ENV=prod"},
		"the manifest has no migrate container": {edit: func(f *migrationFixture) {
			f.m.fileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "- name: migrate", "- name: other", 1), true, nil
			}
		}, want: "0 containers named migrate"},
		"the manifest has a fixed name": {edit: func(f *migrationFixture) {
			f.m.fileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "generateName: agentrt-migrate-", "name: agentrt-migrate", 1), true, nil
			}
		}, want: "no generateName"},
		"the manifest is not a Job": {edit: func(f *migrationFixture) {
			f.m.fileAt = func(context.Context, string, string, string) (string, bool, error) {
				return "apiVersion: v1\nkind: Pod\nmetadata:\n  generateName: x-\n  namespace: platform-prod-ns\n", true, nil
			}
		}, want: "not a batch/v1 Job"},
		"the Job fails": {edit: func(f *migrationFixture) {
			f.finish, f.log = batchv1.JobFailed, "agentrt-migrate: migrate: 0002_notes.sql: ERROR: agentrt.note has 1 row(s)\n"
		}, want: "failed; every migration file runs in one transaction, so a file that failed changed nothing. Log tail:\nagentrt-migrate: migrate: 0002_notes.sql: ERROR: agentrt.note has 1 row(s)", created: 1},
		"the Job is still running when the wait ends": {edit: func(f *migrationFixture) { f.finish = "" },
			want: "still running after 200ms", created: 1, uncertain: true},
		"the Job completes without the migrator's line": {edit: func(f *migrationFixture) { f.log = "ready\n" },
			want: "does not carry the agentrt-migrate line for prod", created: 1, uncertain: true},
		"the line is another environment's": {edit: func(f *migrationFixture) {
			f.log = "agentrt-migrate: env=stg database=bifrost_platform_stg applied=[2] seeded=false\n"
		}, want: "does not carry the agentrt-migrate line for prod", created: 1, uncertain: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMigrationFixture(t)
			if c.edit != nil {
				c.edit(f)
			}
			commit := c.commit
			if commit == "" {
				commit = migCommit
			}
			out, err := f.m.run(context.Background(), commit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("answer = %v, error = %v, want %q", out, err, c.want)
			}
			if len(f.created) != c.created || actions.IsUncertain(err) != c.uncertain {
				t.Fatalf("created %d jobs (want %d), uncertain = %v (want %v): %v", len(f.created), c.created, actions.IsUncertain(err), c.uncertain, err)
			}
		})
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// What the policy reads of this platform is what /health reports: the
// applied migrations by number and sha256; an unread schema is an error.
func TestOwnMigrationsReadTheSchemaHealthReports(t *testing.T) {
	s := &Server{}
	if _, err := (ownMigrations{s}).Own(context.Background()); err == nil {
		t.Fatal("a platform without the runtime answered")
	}
	s.agentRuntime = &agentRuntime{env: "prod", schema: &schemaWatch{}}
	if _, err := (ownMigrations{s}).Own(context.Background()); err == nil || !strings.Contains(err.Error(), "have not been read") {
		t.Fatalf("an unread schema: %v", err)
	}
	s.agentRuntime.schema.status = &migrate.SchemaStatus{State: migrate.SchemaCurrent, Applied: []migrate.Applied{{ID: 1, SHA256: "a"}, {ID: 2, SHA256: "b"}}}
	applied, ok := s.agentRuntime.schema.applied()
	if !ok || len(applied) != 2 || applied[2] != "b" {
		t.Fatalf("applied = %v, %v", applied, ok)
	}
	s.agentRuntime.schema.err = "connection refused"
	if _, ok := s.agentRuntime.schema.applied(); ok {
		t.Fatal("a failed read still answered")
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
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

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
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

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
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
