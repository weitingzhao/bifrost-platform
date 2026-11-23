// Package mustrefuse_test is the must-refuse set: requests the release
// policy engine refuses whatever else about it changes.
//
// Only the Owner edits this directory. The policy he signs lists it under
// owner_only, so a release whose diff touches it is never covered. An author
// who changes the engine runs these tests; if one of the cases becomes
// covered, the change is wrong, not the case.
package mustrefuse_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const (
	stg          = "bifrost-deliver-stg"
	platformStg  = "bifrost-deliver-platform"
	platformProd = "bifrost-deliver-platform-prod"
	tradeProd    = "bifrost-deliver-prod"

	core, worker, infra, platform, ui = "bifrost-trade-core", "bifrost-trade-worker", "bifrost-trade-infra", "bifrost-platform", "bifrost-ui"

	requester = "local_requester"
	planID    = "plan-1"
	applier   = "bifrost-applier"

	// The platform's migrations: 0001 is applied on PROD and in the build
	// PROD runs; 0002 is new in the commit being released.
	migrations     = "api/internal/agentrt/migrations"
	appliedFile    = migrations + "/0001_core.sql"
	newFile        = migrations + "/0002_notes.sql"
	appliedSQL     = "CREATE TABLE agentrt.task (task_id text PRIMARY KEY);\n"
	reversibleSQL  = "CREATE TABLE agentrt.note (note_id bigint PRIMARY KEY);\n"
	reversibleCase = "Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs)"
)

var (
	now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	// stgRepos are the repos a Trade STG delivery ships.
	stgRepos = []string{core, worker, "bifrost-trade-api", "bifrost-trade-frontend", ui, infra}
)

// before and after are a repo's deployed commit and the one being released.
func before(repo string) string { return rptest.SHA("o" + repo[len("bifrost-"):]) }
func after(repo string) string  { return rptest.SHA("n" + repo[len("bifrost-"):]) }

// request is one of the five requests the set starts from. Each is covered
// in an untouched world (TestTheStartingRequestsAreCovered), so a case's
// refusal comes from what the case changed.
type request int

const (
	releaseStg request = iota
	releasePlatform
	releasePlatformProd
	applyPlan
	// runMigration asks the PROD platform to run the migrations of the
	// commit STG delivered (run_agentrt_migration).
	runMigration
)

// world is everything the engine reads, in the state where all four
// requests are covered.
type world struct {
	t      *testing.T
	key    rptest.Key
	cms    *rptest.ConfigMaps
	facts  *rptest.Facts
	plan   releasepolicy.ApplyPlan
	live   *rptest.Live
	migs   *rptest.Migrations
	clock  time.Time
	tier   releasepolicy.Tier
	params map[string]any // set by a case to ask something else than its starting request
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, key: rptest.NewKey(t), cms: rptest.NewConfigMaps(), clock: now, tier: releasepolicy.Tier("C")}
	w.sign(w.key, w.policyText())
	w.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	w.facts = &rptest.Facts{
		Heads: map[string]string{}, Files: map[string][]string{}, Green: map[string]bool{}, Text: map[string]string{},
		Dirs: map[string][]string{infra + "@main:scripts/release/db-steps.d": {"README.md"}},
		Records: map[string]map[string]string{
			stg:          {},
			platformStg:  {platform: before(platform), ui: before(ui)},
			platformProd: {platform: before(platform), ui: before(ui)},
		},
		// A successful STG delivery of the commits the PROD request pins.
		Older: map[string][]releasepolicy.Shipment{platformStg: {{Repos: map[string]string{platform: after(platform), ui: after(ui)}, Succeeded: true}}},
	}
	for _, repo := range append([]string{platform}, stgRepos...) {
		w.facts.Heads[repo] = after(repo)
		w.facts.Green[repo+"@"+after(repo)] = true
		w.facts.Files[repo] = []string{"docs/readme.md"}
	}
	for _, repo := range stgRepos {
		w.facts.Records[stg][repo] = before(repo)
	}
	// The platform's own migrations, as PROD reads them: it runs the build
	// before, has applied 0001, and the commit STG delivered adds 0002.
	sum := sha256.Sum256([]byte(appliedSQL))
	w.migs = &rptest.Migrations{State: releasepolicy.OwnMigrations{
		Pipeline: platformProd, Repo: platform, Dir: migrations, Applied: map[int]string{1: hex.EncodeToString(sum[:])},
	}}
	w.facts.Dirs[platform+"@"+after(platform)+":"+migrations] = []string{"0001_core.sql", "0002_notes.sql", "embed.go"}
	w.facts.Text[platform+"@"+before(platform)+":"+appliedFile] = appliedSQL
	w.facts.Text[platform+"@"+after(platform)+":"+appliedFile] = appliedSQL
	w.facts.Text[platform+"@"+after(platform)+":"+newFile] = reversibleSQL
	w.plan = releasepolicy.ApplyPlan{
		Ready: true, Policy: "pass", Repo: infra, Path: "k8s/cicd/tekton/pipeline-ci-platform.yaml", Commit: after(infra),
		Objects: []releasepolicy.ApplyObject{{Namespace: "cicd", Kind: "Pipeline", Name: "bifrost-ci-platform", Ownership: "ssa"}},
	}
	w.live = &rptest.Live{Err: map[string]error{}, Objects: map[string][]rptest.LiveManager{
		"cicd/Pipeline/bifrost-ci-platform": {{Manager: applier, Operation: "Apply"}},
	}}
	return w
}

// policyText is testdata/policy.json signed an hour ago for 90 days.
func (w *world) policyText() string {
	w.t.Helper()
	raw, err := os.ReadFile("testdata/policy.json")
	if err != nil {
		w.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		w.t.Fatal(err)
	}
	doc["policy_id"] = "rp-must-refuse"
	doc["signed_at"] = now.Add(-time.Hour).Format(time.RFC3339)
	doc["expires_at"] = now.Add(90 * 24 * time.Hour).Format(time.RFC3339)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		w.t.Fatal(err)
	}
	return string(out) + "\n"
}

func (w *world) sign(k rptest.Key, text string) {
	w.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
		"policy.yaml": text, "policy.sig": k.Sign(w.t, []byte(text), releasepolicy.NamespacePolicy),
	})
}

// object replaces the plan's one object and says the applier applied it before.
func (w *world) object(namespace, kind, name string) {
	w.plan.Objects = []releasepolicy.ApplyObject{{Namespace: namespace, Kind: kind, Name: name, Ownership: "ssa"}}
	w.live.Objects[namespace+"/"+kind+"/"+name] = []rptest.LiveManager{{Manager: applier, Operation: "Apply"}}
}

// migration makes the Trade STG release change one migration of core from
// was to is; is "" deletes the file.
func (w *world) migration(was, is string) {
	const path = "src/bifrost_core/persistence/migrations/0042_orders.sql"
	w.facts.Files[core] = []string{path}
	w.facts.Text[core+"@"+before(core)+":"+path] = was
	if is != "" {
		w.facts.Text[core+"@"+after(core)+":"+path] = is
	}
}

// platformMigration makes the platform's release carry the new migration
// 0002 with the text sql; run_agentrt_migration reads the same file.
func (w *world) platformMigration(sql string) {
	w.facts.Files[platform] = []string{newFile}
	w.facts.Text[platform+"@"+after(platform)+":"+newFile] = sql
}

type plans struct{ w *world }

func (p plans) ApplyPlan(_ context.Context, id string) (releasepolicy.ApplyPlan, error) {
	if id != planID {
		return releasepolicy.ApplyPlan{}, errors.New("not found")
	}
	return p.w.plan, nil
}

type live struct{ l *rptest.Live }

func (s live) Managers(_ context.Context, namespace, kind, name string) ([]releasepolicy.FieldManager, bool, error) {
	list, found, err := s.l.Read(namespace, kind, name)
	out := make([]releasepolicy.FieldManager, 0, len(list))
	for _, m := range list {
		out = append(out, releasepolicy.FieldManager{Manager: m.Manager, Operation: m.Operation})
	}
	return out, found, err
}

// decide asks the real engine for the request, or for w.params when a case set them.
func (w *world) decide(r request) releasepolicy.Decision {
	eng := releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: w.cms, Writer: rptest.Writer{CMs: w.cms}, Git: w.facts, CI: w.facts, Window: w.facts, Deployed: w.facts,
		Plans: plans{w}, Live: live{w.live}, Migrations: w.migs, Anchor: w.key.Fingerprint, Now: func() time.Time { return w.clock },
	})
	action, params := "start_pipeline_run", map[string]any{"revision": "main", "who": requester}
	switch r {
	case releaseStg:
		params["name"] = stg
	case releasePlatform:
		params["name"] = platformStg
	case releasePlatformProd:
		params["name"], params["revision"] = platformProd, after(platform)
		params["params"] = map[string]string{"uiRevision": after(ui)}
	case applyPlan:
		action, params = "apply_manifest", map[string]any{"plan_id": planID}
	case runMigration:
		action, params = "run_agentrt_migration", map[string]any{"commit": after(platform)}
	}
	if w.params != nil {
		params = w.params
	}
	return eng.Decide(context.Background(), action, w.tier, params)
}
