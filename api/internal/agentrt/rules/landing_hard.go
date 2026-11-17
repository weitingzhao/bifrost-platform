package rules

import (
	"path"
	"slices"
	"strings"
)

// Coordinator v3 §4.2 / §12.1. Prefixes protect entire packages, including
// future files, subpackages, tests, fixtures and build-tagged sources. These
// are the permission paths: a change to one needs the Owner's core approval.
var landingHardGo = [...]string{
	// The landing gate and its CI.
	"api/internal/agentrt/rules/",
	"api/internal/agentrt/landing/",
	"api/internal/agentrt/landinggit/",
	"api/internal/agentrt/landingci/",
	// The Owner's approvals and what they actuate.
	"api/internal/approvals/",
	"api/internal/approvalhome/",
	"api/internal/approvalnotify/",
	"api/internal/actions/",
	"api/internal/actuation/",
	"api/internal/actuationpolicy/",
	"api/internal/releasepolicy/",
	"api/internal/workactions/",
	// The runner that executes an owner_run_command on a one-time code.
	"api/internal/ownerexec/",
	"api/cmd/owner-exec/",
	// Assembly, routes and identity configuration.
	"api/internal/server/",
	"api/internal/agentrt/agentrtapi/",
	"api/internal/config/",
	"api/cmd/platform-api/",
	"api/cmd/operator-plane/",
	// The schema and the agent host.
	"api/internal/agentrt/migrate/",
	"api/internal/agentrt/migrations/",
	"api/internal/agentrt/plan/",
	"api/internal/agentrt/infracheck/",
	"api/cmd/infracheck/",
	// Held by the closure of the roots above, or by agentd and the runtime API.
	"api/internal/agentrt/canon/",
	"api/internal/statefile/",
	"api/internal/notice/",
	"api/internal/threadinbox/",
	"api/internal/assessmentview/",
	"api/internal/safego/",
	"api/internal/maintainer/",
	"api/internal/opscontext/",
	"api/internal/localpath/",
	// Signed-rule rows the open test harness inserts; they name the rule
	// table (TestLandingOpenPathsNameNoPermissionRecord).
	"api/internal/agentrt/agentrttest/rules.go",
}

// The agent runtime's store is split by file. A file in neither map, and any
// subdirectory, is permission. Each file is classified by one test: can it
// widen what agents may do without the Owner? Keys are basenames; a key with
// "*" is a path.Match pattern.
const landingStore = "api/internal/agentrt/store/"

var landingStorePermission = map[string]string{
	"pin*":                 "pinned checks and their reviews: what a task's commit must pass",
	"rules.go":             "stores and reads the Owner's signed rules",
	"envelope.go":          "a task's envelope: what its attempt may touch",
	"propose.go":           "agents' proposals of missions and tasks",
	"signoff.go":           "records the Owner's sign-off on a step",
	"acceptancesignoff.go": "an acceptance sign-off makes a step done",
	"cardauto.go":          "answers cards without the Owner, by rule or assessment",
	"cardkinds.go":         "card kinds and the answers each accepts",
	"claim.go":             "a host claiming a task's attempt",
	"examstream.go":        "names the stream of the removed exams, whose artifacts stay hidden from Agent hosts",
	"event.go":             "the append-only event log and its checks",
	"write.go":             "writer identity and idempotent writes",
	"vocab.go":             "the event types each writer may append",
	"keyed.go":             "idempotency keys of requests",
	"alerts.go":            "records policy alerts and tells the Owner of them",
	"artifactsearch.go":    "artifact search; hides exam artifacts from Agent hosts",
	"assess.go":            "assessments, including whether a signed rule already answers an item",
	"assessrequest.go":     "starts and ends assessment requests",
	"attempt.go":           "an attempt's fenced start, heartbeat, checkpoint and result",
	"benchmark.go":         "benchmark runs that scores and routing read",
	"blockedcard.go":       "asks and decides blocked-task cards",
	"blockedclose.go":      "closes a mission and cancels its work on a card's answer",
	"briefdue.go":          "schedules briefs and the Owner's nudges",
	"claims.go":            "detects claims of the Owner's approval in messages",
	"conclusion.go":        "posts the platform's own conclusions",
	"eventcursor.go":       "event reads and the events hidden from intake clients",
	"externalwork.go":      "collector identity and external work bindings",
	"facts.go":             "records external facts and evaluates steps",
	"goal.go":              "proposes a goal's mission and its planning task",
	"hostdisk.go":          "the disk floor below which a host gets no new work",
	"ids.go":               "mints task, attempt, message and control ids",
	"kernel.go":            "the mission, task and attempt state machine",
	"localpath.go":         "the artifact kinds that skip the local-path scan",
	"loopalert.go":         "tells the Owner of failing controller loops",
	"planbindings.go":      "installs plan bindings and bound done rules",
	"planchange.go":        "applies plan changes",
	"plandecide.go":        "decides plan changes on the Owner's cards",
	"planfactssnapshot.go": "the plan facts bound done rules read",
	"planimport.go":        "imports a plan tree",
	"planimportcard.go":    "proposes a plan import on an Owner card",
	"plannote.go":          "writes notes on plan rows",
	"planobservations.go":  "collects the observations bound steps read",
	"planproposal.go":      "proposes plan changes on an Owner card",
	"planresult.go":        "accepts or refuses a planner's plan",
	"planview.go":          "adopts plan views and their bindings",
	"preparefailed.go":     "attempts that failed before their executor started",
	"presence.go":          "host offers and presence that routing reads",
	"presencejudge.go":     "presence switches",
	"presencelive.go":      "host liveness that claims read",
	"presencepass.go":      "sets hosts aside and back",
	"progress.go":          "the progress projection; presence reads its HostLostAfter",
	"progressarch.go":      "architecture rows; gates read its recorded deploys",
	"recorderimport.go":    "proposes and decides recorder imports",
	"retry.go":             "failure backoff, holds and handover",
	"seed.go":              "seeds capability facts",
	"waits.go":             "records waits past their limit",
	"workspaceref.go":      "the artifact kinds that skip the workspace-path scan",
}

var landingStoreOpen = map[string]string{
	"planpace.go":             "a round's pace and quiet hours, for the Console; reads only",
	"planverificationview.go": "the plan view's verification column; reads only",
	"planviewrecent.go":       "the plan view's recent changes; reads only",
	"planviewsnapshot.go":     "the plan view, read from one snapshot; reads only",
	"planviewsummary.go":      "the plan view's execution summary; reads only",
	"planevidence.go":         "evidence suggestions in the plan view, never facts; reads only",
	"progresseta.go":          "a mission's ETA in the progress projection; reads only",
	"progressneeds.go":        "Needs you in the progress projection; reads only",
	"progressnext.go":         "when the Owner is next needed, in the progress projection",
	"progressschema.go":       "loop failures and schema state in the progress projection; reads only",
}

// Open paths may change without the Owner: no core, floor_soft or
// presentation refusal, and no rule_paths refusal for being outside allow.
// Deny and size still apply. Like assemblyEdges, each entry names its reason.
var landingOpen = map[string]string{
	"api/internal/agentrt/questioncards/":  "adapts the question service to landing and STG cards; the answers stay the question service's",
	"api/internal/agentrt/controller/":     "runs the runtime's loops through the store's permission code",
	"api/internal/agentrt/agentrttest/":    "test database harness",
	"api/internal/agentrt/planimport/":     "parses the work ledger into an import request; reads and writes nothing",
	"api/internal/agentrt/planview/":       "validates the offline plan-view artifact; grants nothing",
	"api/internal/agentrt/recorderimport/": "checks and masks recorder logs for import",
	"api/internal/questions/":              "stores Owner questions; an answer authorizes nothing",
	"api/internal/cluster/":                "cluster reads and data clone",
	"api/internal/placement/":              "workload placement reads",
	"api/internal/probe/":                  "connectivity probes",
	"api/internal/delivery/":               "delivery pipeline reads and STG windows",
	"api/internal/lineage/":                "commit trailers and lineage",
	"api/internal/releases/":               "records the commits each delivery built",
	"api/cmd/agentrt-plan-import/":         "command around planimport",
	"api/Makefile":                         "developer targets; no credentialed pipeline runs it (the root Makefile, which deploy_mac_mini.sh runs, stays permission)",
	"console/vitest.config.ts":             "test runner configuration",
	"console/tsconfig*.json":               "TypeScript compiler configuration",
}

var landingHardConsole = [...]string{
	"console/package.json",
	"console/package-lock.json",
	"console/vite.config.ts",
	"console/index.html",
	"console/nginx.conf",
	"console/public/sw.js",
	"console/public/manifest.webmanifest",
	"console/src/main.tsx",
	"console/src/App.tsx",
	"console/src/pwa/",
	"console/src/api/approvals.ts",
	"console/src/api/approvalsServerContract.ts",
	"console/src/api/approvalGoStructParse.ts",
	"console/src/api/client.ts",
	"console/src/api/serverContact.ts",
	"console/src/api/cluster.ts",
	"console/src/api/deliveryBoundaries.ts",
	"console/src/pages/shell/agents/DeliveryBoundaries.tsx",
	"console/src/lib/shell/consoleRoutes.ts",
	"console/src/pages/ConsolePage.tsx",
	"console/src/hooks/PlatformAuthProvider.tsx",
	"console/src/hooks/platformAuthContext.ts",
	"console/src/hooks/usePlatformAuth.ts",
	"console/src/lib/platformAuth.ts",
	"console/src/lib/platformAuthUi.ts",
	"console/src/lib/signInError.ts",
	"console/src/pages/ApprovalsPage.tsx",
	"console/src/pages/ApprovalReviewButton.tsx",
	"console/src/pages/OwnerExecRun.tsx",
	"console/src/api/ownerExec.ts",
	"console/src/pages/approvalDecision.ts",
	"console/src/pages/approvalSiblings.ts",
	"console/src/pages/approvalWaitingNotice.ts",
	"console/src/pages/approvalsReadNotice.ts",
	"console/src/pages/CloseUnknownForm.tsx",
	"console/src/pages/decisionContact.ts",
	"console/src/pages/decisionSignIn.ts",
	"console/src/pages/DecisionEnvironment.tsx",
	"console/src/pages/useDecisionEnvironment.ts",
	"console/src/pages/useDecisionPageResume.ts",
	"console/src/pages/serverContactNotice.ts",
	"console/src/pages/ServerContactStatus.tsx",
	"console/src/pages/shell/needs-you/ApprovalPlanSteps.tsx",
	"console/src/pages/shell/needs-you/ApprovalRow.tsx",
	"console/src/pages/shell/needs-you/useNeedsYou.ts",
	"console/src/components/shell/RequestActionButton.tsx",
	// These send or read the operator token (TestLandingHardConsoleCallSites):
	// questions.ts posts the Owner's answers with it. They stay permission.
	"console/src/api/agentRuntimePlan.ts",
	"console/src/api/questions.ts",
	"console/src/pages/shell/releases/ReleasesPage.tsx",
	"console/src/pages/shell/owner/useOwnerConsole.ts",
}

var landingHardOtherDirs = [...]string{
	"mcp/platform/",
	".husky/",
	".github/",
}

var landingHardFiles = [...]string{
	"api/go.mod",
	"api/go.sum",
	// deploy_mac_mini.sh runs it to build the operator plane it deploys.
	"Makefile",
	"config/actions-catalog.json",
	"config/actuation-policy.yaml",
	"config/platform-auth.yaml",
}

var landingHardBasenames = [...]string{
	"package.json",
	"package-lock.json",
	"go.mod",
	"go.sum",
	".gitattributes",
	".gitmodules",
}

func inLandingEntries(p string, entries []string) bool {
	for _, entry := range entries {
		if p == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(p, entry) {
			return true
		}
	}
	return false
}

// landingStoreEntry is the store map key a file directly under the store
// matches, permission first, and whether that key is open. A subdirectory
// or an unlisted file matches no key and is permission.
func landingStoreEntry(p string) (key string, open bool) {
	name, ok := strings.CutPrefix(p, landingStore)
	if !ok || strings.Contains(name, "/") {
		return "", false
	}
	for key := range landingStorePermission {
		if ok, _ := path.Match(key, name); ok {
			return key, false
		}
	}
	if _, ok := landingStoreOpen[name]; ok {
		return name, true
	}
	return "", false
}

// landingOpenEntry: p is on the open list. A "/" entry is a prefix, one with
// "*" a path.Match pattern, any other an exact path.
func landingOpenEntry(p string) bool {
	if _, open := landingStoreEntry(p); open {
		return true
	}
	for entry := range landingOpen {
		if ok, _ := path.Match(entry, p); ok || inLandingEntries(p, []string{entry}) {
			return true
		}
	}
	return false
}

// LandingOpen: p may change without the Owner's core approval or an allow
// match (LandingSoftRefusals). Permission entries win over the open list.
func LandingOpen(repo, p string) bool {
	return repo == "bifrost-platform" && !LandingHardAbsolute(repo, p) && !LandingHard(repo, p) && landingOpenEntry(p)
}

// LandingHard is the union of core and absolute. Unknown repositories remain
// absolute until S6 supplies their explicit policy; they cannot receive grants.
func LandingHard(repo, p string) bool {
	if repo == "bifrost-trade-infra" {
		return !landingPath(p) || infraHard(p)
	}
	if LandingHardAbsolute(repo, p) {
		return true
	}
	if _, open := landingStoreEntry(p); strings.HasPrefix(p, landingStore) && !open {
		return true
	}
	return inLandingEntries(p, landingHardGo[:]) || inLandingEntries(p, landingHardConsole[:]) ||
		inLandingEntries(p, landingHardOtherDirs[:]) || inLandingEntries(p, landingHardFiles[:]) ||
		slices.Contains(landingHardBasenames[:], path.Base(p)) || strings.HasPrefix(path.Base(p), "Dockerfile")
}

// LandingHardAbsolute: for infra only the never-grantable paths; its
// floor-grantable paths wait for approve_floor_landing (LandingFloorGrant).
func LandingHardAbsolute(repo, p string) bool {
	if repo == "bifrost-trade-infra" {
		return InfraNeverGrantable(p)
	}
	return repo != "bifrost-platform" || !landingPath(p) || strings.HasPrefix(path.Base(p), ".env") ||
		path.Base(p) == ".gitmodules" || strings.HasPrefix(p, ".github/")
}
