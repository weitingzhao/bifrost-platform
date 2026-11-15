package mustrefuse_test

import (
	"errors"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// caseCount is how many cases the set has. Cases are added, with this
// number; a case is never dropped to make a change pass.
const caseCount = 40

// mustRefuse is one request the engine must refuse: the starting request,
// what is wrong with it, and words its refusal must carry. origin says where
// the case comes from.
type mustRefuse struct {
	name   string
	origin string
	start  request
	wrong  func(w *world)
	want   string
}

const (
	d10       = "D10, signed 2026-07-04: trading execution is frozen; nothing that touches it ships without the Owner"
	tierD     = "ADR section 5 (2026-10-08): DDL that drops, rewrites or retypes data is tier D"
	rules     = "W-42 (2026-10-15): the rules, their gates and the Owner's key change only by the Owner's hand"
	selfCheck = "Owner 2026-11-15: no second reader; the author proves a judge change with machine facts (must-refuse set, decision snapshots)"
	class4    = "Owner 2026-11-14: only applies with a way back are covered (owner-off-the-loop-rules, class 4)"
	base      = "W-42 (2026-10-08): the conditions every auto-approved release holds"
)

func files(repo string, list ...string) func(w *world) {
	return func(w *world) { w.facts.Files[repo] = list }
}

func sql(statement string) func(w *world) {
	return func(w *world) {
		w.migration("CREATE TABLE orders (id bigint);\n", "CREATE TABLE orders (id bigint);\n"+statement+"\n")
	}
}

var cases = []mustRefuse{
	// The trading freeze.
	{"d10: the daemon's order execution", d10, releaseStg,
		files(worker, "src/bifrost_worker/daemon/execution/place_order.py"), "hits no_d10_paths: src/bifrost_worker/daemon/execution/place_order.py"},
	{"d10: the STG daemon scale-zero patch", d10, releaseStg,
		files(infra, "k8s/overlays/stg/daemon-scale-zero.patch.yaml"), "hits no_d10_paths: k8s/overlays/stg/daemon-scale-zero.patch.yaml"},
	{"d10: the IB operator command stream", d10, releaseStg,
		files(core, "src/bifrost_core/ib_operator/commands.py"), "hits no_d10_paths: src/bifrost_core/ib_operator/commands.py"},
	{"d10: a path that is also a test file", d10, releaseStg,
		files(worker, "src/bifrost_worker/daemon/fsm/machine_test.go"), "hits no_d10_paths"},

	// Migrations that delete or rewrite data, with additive_ddl on.
	{"ddl: DROP TABLE", tierD, releaseStg, sql("DROP TABLE orders;"), "not additive-only (tier D, ask the Owner)"},
	{"ddl: DELETE FROM", tierD, releaseStg, sql("DELETE FROM orders WHERE id < 100;"), "adds DELETE FROM"},
	{"ddl: UPDATE SET", tierD, releaseStg, sql("UPDATE orders SET id = id + 1;"), "adds UPDATE ORDERS SET"},
	{"ddl: TRUNCATE", tierD, releaseStg, sql("TRUNCATE orders;"), "adds TRUNCATE"},
	{"ddl: ALTER COLUMN TYPE", tierD, releaseStg, sql("ALTER TABLE orders ALTER COLUMN id TYPE text;"), "adds ALTER COLUMN"},
	{"ddl: a statement rewritten in place", tierD, releaseStg,
		func(w *world) { w.migration("CREATE TABLE orders (id bigint);\n", "CREATE TABLE orders (id text);\n") }, "removes or rewrites"},
	{"ddl: a migration deleted", tierD, releaseStg,
		func(w *world) { w.migration("CREATE TABLE orders (id bigint);\n", "") }, "is deleted"},

	// Pipelines the policy does not allow.
	{"allow: Trade PROD", "W-42: Trade PROD is not in the allow list; it ships on a card", releasePlatformProd,
		func(w *world) {
			w.params = map[string]any{"name": tradeProd, "who": requester, "params": map[string]string{"coreRevision": after(core)}}
		}, tradeProd + " is not in the policy's allow list"},
	{"allow: a pipeline nobody listed", base, releasePlatform,
		func(w *world) {
			w.params = map[string]any{"name": "bifrost-deliver-elsewhere", "revision": "main", "who": requester}
		}, "is not in the policy's allow list"},
	{"tier D is never covered", "ADR section 5: tier D always waits for the Owner", releasePlatform,
		func(w *world) { w.tier = actions.TierD }, "tier D is never auto-approved"},

	// The rules themselves, in a repo no self-check covers.
	{"rules: infra's release-policy template and paths", rules, releaseStg,
		files(infra, "agent-config/release-policy/paths.json"), "hits no_trust_anchor_change: agent-config/release-policy/paths.json"},
	{"rules: the agent guard", rules, releaseStg,
		files(infra, "agent-config/scripts/agent-guard/preflight.js"), "hits no_trust_anchor_change"},
	{"rules: infra's own test files are not left out", rules, releaseStg,
		files(infra, "scripts/release/test_freeze_gate.py", "agent-config/release-policy/testdata/x.json"), "hits no_trust_anchor_change"},

	// Owner-only paths, with the self-check on.
	{"owner-only: this directory", selfCheck, releasePlatform,
		files(platform, "api/internal/releasepolicy/engine.go", "api/internal/releasepolicy/mustrefuse/cases_test.go"),
		"touches owner-only paths: api/internal/releasepolicy/mustrefuse/cases_test.go (only the Owner changes these)"},
	{"owner-only: this directory's policy, a testdata file", selfCheck, releasePlatform,
		files(platform, "api/internal/releasepolicy/mustrefuse/testdata/policy.json"), "touches owner-only paths"},
	{"owner-only: the Owner's key", selfCheck, releasePlatform,
		files(platform, "api/internal/releasepolicy/anchor.go"), "touches owner-only paths: api/internal/releasepolicy/anchor.go"},
	{"owner-only: hidden past the hundredth file of a large diff", "2026-11-14: 5 of 7 large PROD diffs hid a trust anchor behind the mirror's cap of 100 files", releasePlatform,
		func(w *world) {
			many := make([]string, 120)
			for i := range many {
				many[i] = "docs/page" + strings.Repeat("i", i) + ".md"
			}
			w.facts.Files[platform] = many
			w.facts.TreeFiles = map[string][]string{platform: append(many[:len(many):len(many)], "api/internal/releasepolicy/mustrefuse/world_test.go")}
		}, "touches owner-only paths"},
	{"self-check: a trust-anchor change whose CI did not succeed", selfCheck, releasePlatform,
		func(w *world) {
			w.facts.Files[platform] = []string{"api/internal/releasepolicy/engine.go"}
			w.facts.Green = nil
		}, "hits no_trust_anchor_change: api/internal/releasepolicy/engine.go (self-checked only with a Succeeded ci run"},

	// The policy and the freeze.
	{"frozen", "W-42: a freeze overrides the policy", releasePlatform,
		func(w *world) {
			w.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-11-15T11:00:00Z", "who": "owner", "reason": "incident"})
		}, "releases are frozen"},
	{"the freeze record is gone", "W-42: an unreadable or missing freeze counts as frozen", releasePlatform,
		func(w *world) { w.cms.Set(releasepolicy.FreezeConfigMap, nil) }, "releases are frozen"},
	{"frozen: an apply", class4, applyPlan,
		func(w *world) {
			w.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-11-15T11:00:00Z", "who": "owner", "reason": "incident"})
		}, "releases are frozen"},
	{"expired policy", "W-42: a policy is signed for 90 days", releasePlatform,
		func(w *world) { w.clock = now.Add(91 * 24 * time.Hour) }, "expired at"},
	{"policy text changed after signing", "W-42: only the Owner's signature makes a policy", releasePlatform,
		func(w *world) {
			text := w.policyText()
			w.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
				"policy.yaml": strings.Replace(text, `"valid_days": 90`, `"valid_days": 900`, 1),
				"policy.sig":  w.key.Sign(w.t, []byte(text), releasepolicy.NamespacePolicy),
			})
		}, "policy signature"},
	{"policy signed by another key", "W-42: only the Owner's signature makes a policy", releasePlatform,
		func(w *world) { w.sign(rptest.NewKey(w.t), w.policyText()) }, "not the compiled-in Owner key"},

	// The facts of a release.
	{"CI did not succeed", base, releasePlatform, func(w *world) { w.facts.Green = nil }, "has no Succeeded ci run"},
	{"the window is held by someone else", "2026-09-27: two sessions released core 0.24.0 at once", releasePlatform,
		func(w *world) { w.facts.Window = "REFUSED: release window held by local_other" }, "release window: REFUSED"},
	{"a large diff that cannot be read whole", "2026-11-14: a diff of 100 files or more may be cut short and must not be judged covered on the part that was read", releasePlatform,
		func(w *world) {
			many := make([]string, 100)
			for i := range many {
				many[i] = "docs/page" + strings.Repeat("i", i) + ".md"
			}
			w.facts.Files[platform] = many
		}, "the complete difference cannot be read"},
	{"a diff that cannot be read", base, releasePlatform,
		func(w *world) { w.facts.FilesErr = errors.New("status 500") }, "cannot read the diff"},
	{"PROD commits STG never delivered", "2026-11-11: PROD ships only what a successful STG delivery shipped", releasePlatformProd,
		func(w *world) { w.facts.Older = nil }, "was never delivered successfully by " + platformStg},

	// Applies.
	{"apply: a Secret", class4, applyPlan, func(w *world) { w.object("cicd", "Secret", "gitea-token") }, "outside the policy's apply namespaces and kinds: cicd/Secret/gitea-token"},
	{"apply: a Role", class4, applyPlan, func(w *world) { w.object("cicd", "Role", "runner") }, "outside the policy's apply namespaces and kinds"},
	{"apply: a ClusterRole", class4, applyPlan, func(w *world) { w.object("", "ClusterRole", "runner") }, "outside the policy's apply namespaces and kinds"},
	{"apply: a RoleBinding", class4, applyPlan, func(w *world) { w.object("cicd", "RoleBinding", "runner") }, "outside the policy's apply namespaces and kinds"},
	{"apply: an object in another namespace", class4, applyPlan,
		func(w *world) { w.object("bifrost-prod", "Pipeline", "bifrost-ci-platform") }, "outside the policy's apply namespaces and kinds: bifrost-prod/Pipeline/bifrost-ci-platform"},
	{"apply: a PROD delivery pipeline", class4, applyPlan,
		func(w *world) { w.object("cicd", "Pipeline", platformProd) }, "excluded by the policy whatever renders it: cicd/Pipeline/" + platformProd},
	{"apply: a first creation", class4, applyPlan,
		func(w *world) { delete(w.live.Objects, "cicd/Pipeline/bifrost-ci-platform") }, "does not exist in the cluster: a first creation"},
}
