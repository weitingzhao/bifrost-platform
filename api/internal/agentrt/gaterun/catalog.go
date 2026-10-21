package gaterun

import (
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// The gates of the architecture table's rows (ARCH, planimport.ArchRootID),
// reviewed with the code they run: what a row's done rule names when the
// plan gives it its gates (TD-397). A row with no gates here has none yet:
// it is not built, or no test of it runs without what an agent host does
// not have (the chief exam's paper). Every test named here is one that runs
// wherever the runner runs, given the test database, and never skips itself
// (catalog_test.go reads them from the source).
var Catalog = []Row{
	{Label: "owner-three-duties", Gates: []Gate{{ID: "T40", Text: "The Owner's three duties: a decision card's fields, and an answer that is never an approval.",
		Commands: []Command{{Pkg: "./internal/questions/", Tests: []string{"TestDecisionCardFields", "TestAuthorizationAndAnswerIsNotApproval"}}}}}},
	{Label: "release-guard", Gates: []Gate{{ID: "T41", Text: "The release guard: a pinned release ships what its source shipped, and a policy approves naming its commits.",
		Commands: []Command{{Pkg: "./internal/releasepolicy/", Tests: []string{"TestPinnedReleaseMustShipWhatTheSourceShipped", "TestValidPolicyAutoApprovesAndNamesTheCommits"}}}}}},
	{Label: "rule-sets", Gates: []Gate{{ID: "T42", Text: "Rule sets: a rule parses or is refused, tightening is checked, a signed tightening never raises, an expired rule grants nothing.",
		Commands: []Command{{Pkg: "./internal/agentrt/rules/", Tests: []string{"TestTightens", "TestParseRefuses"}},
			{Pkg: "./internal/agentrt/store/", Tests: []string{"TestSignedTighteningDoesNotRaise", "TestExpiredRulesGrantNothing"}}}}}},
	{Label: "state-db", Gates: []Gate{{ID: "T43", Text: "The state database: migrations keep the events and grant each environment its own runtime; a tampered event is detected.",
		Commands: []Command{{Pkg: "./internal/agentrt/migrate/", Tests: []string{"TestUpgradeTo0006KeepsEvents", "TestEachEnvironmentGrantsItsOwnRuntimeOnly"}},
			{Pkg: "./internal/agentrt/store/", Tests: []string{"TestTamperIsDetected", "TestIdentitiesAndTheEventRule"}}}}}},
	{Label: "plan-in-runtime", Gates: []Gate{{ID: "T44", Text: "The plan in the runtime: the done-rule grammar is closed, a gate names its exact test, the plan's numbers are fixed.",
		Commands: []Command{{Pkg: "./internal/agentrt/plan/", Tests: []string{"TestGrammarIsClosed", "TestGateUsesTheExactTestID"}},
			{Pkg: "./internal/agentrt/store/", Tests: []string{"TestPlanNumberingIsFixed"}}}}}},
	{Label: "artifact-store", Gates: []Gate{{ID: "T45", Text: "The artifact store: what kinds an upload may be, and two uploads of one body at once.",
		Commands: []Command{{Pkg: "./internal/agentrt/agentrtapi/", Tests: []string{"TestUploadKinds", "TestUploadRace"}}}}}},
	{Label: "budget-quota-ledger", Gates: []Gate{{ID: "T46", Text: "Budgets and quotas: a mission's budget bounds its attempts, a spent budget blocks its ready task, a refused pool cools.",
		Commands: []Command{{Pkg: "./internal/agentrt/store/", Tests: []string{"TestMissionBudget", "TestSpentBudgetBlocksReadyTask", "TestRefusalCoolsThePoolAndClaimsPassTheToolBy"}}}}}},
	{Label: "bus", Gates: []Gate{{ID: "T47", Text: "The bus: the outbox resends once after a crash, rebuilds then delivers; actions are kept and expire.",
		Commands: []Command{{Pkg: "./internal/agentrt/relay/", Tests: []string{"TestCrashBetweenXAddAndMarkResendsOnce", "TestRunRebuildsThenDelivers"}},
			{Pkg: "./internal/agentrt/actions/", Tests: []string{"TestStoreAppendsReadsAndExpires"}}}}}},
	{Label: "runtime-kernel", Gates: []Gate{{ID: "T48", Text: "The runtime kernel: fencing and the lease clock, the lease sweep, a result's path, only the listed moves.",
		Commands: []Command{{Pkg: "./internal/agentrt/store/", Tests: []string{"TestFenceAndLeaseClock", "TestLeaseSweep", "TestResultPath", "TestOnlyListedMoves"}}}}}},
	{Label: "agentd", Gates: []Gate{{ID: "T49", Text: "agentd: it claims through the platform's routes and waits for the platform when it hands over.",
		Commands: []Command{{Pkg: "./internal/agentd/", Tests: []string{"TestClientPath", "TestHandOverWaitsForThePlatform"}}}}}},
	{Label: "dispatch-record", Gates: []Gate{{ID: "T50", Text: "The dispatch record: a route decision replays, and a claim takes only the route.",
		Commands: []Command{{Pkg: "./internal/agentrt/store/", Tests: []string{"TestRouteDecisionReplays", "TestClaimTakesOnlyTheRoute"}}}}}},
	{Label: "progress-in-prod", Gates: []Gate{{ID: "T51", Text: "Progress from the runtime's state: the projection, with or without tasks.",
		Commands: []Command{{Pkg: "./internal/agentrt/store/", Tests: []string{"TestProgress", "TestProgressMissionWithoutTasks"}}}}}},
	{Label: "multi-vendor-routing", Gates: []Gate{{ID: "T52", Text: "Routing across vendors: claims follow the route and its restrictions; a vendor's spent window cools its pool.",
		Commands: []Command{{Pkg: "./internal/agentrt/store/", Tests: []string{"TestClaimRouting", "TestRerouteFollowsRestrictions", "TestAVendorReadingOfASpentWindowCoolsUntilItsReset"}}}}}},
}

// Row is an architecture row's gates; Label is the row's label (its node is
// ARCH:<label>).
type Row struct {
	Label string
	Gates []Gate
}

// ArchRowPrefix is what an architecture row's node id starts with.
const ArchRowPrefix = "ARCH:"

// RowFor is the catalog's row of an architecture node, if it has one.
func RowFor(node string) (Row, bool) {
	for _, r := range Catalog {
		if ArchRowPrefix+r.Label == node {
			return r, true
		}
	}
	return Row{}, false
}

// Rule is the done rule the catalog gives a row in env: gates_passed with
// each gate's test id and spec sha256, as canonical JSON.
func (r Row) Rule(env string) ([]byte, error) {
	if len(r.Gates) == 0 {
		return nil, fmt.Errorf("gaterun: row %s has no gates", r.Label)
	}
	gates := []any{}
	for _, g := range r.Gates {
		_, sum, err := g.Spec()
		if err != nil {
			return nil, err
		}
		gates = append(gates, map[string]any{"test": g.ID, "spec_sha256": sum})
	}
	return canon.Encode(map[string]any{"gates_passed": gates, "env": env})
}

// Specs is every gate's spec by its sha256.
func Specs() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, r := range Catalog {
		for _, g := range r.Gates {
			body, sum, err := g.Spec()
			if err != nil {
				return nil, err
			}
			out[sum] = body
		}
	}
	return out, nil
}
