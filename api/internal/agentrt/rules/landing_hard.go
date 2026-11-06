package rules

import (
	"path"
	"slices"
	"strings"
)

// Coordinator v3 §4.2 / §12.1. Prefixes protect entire packages, including
// future files, subpackages, tests, fixtures and build-tagged sources. These
// are permission paths: a change to one can widen what agents may do without
// the Owner, so it needs the Owner's core approval (hard_core).
var landingHardGo = [...]string{
	// The landing gate itself: its rules, git reader and CI evidence.
	"api/internal/agentrt/rules/",
	"api/internal/agentrt/landing/",
	"api/internal/agentrt/landinggit/",
	"api/internal/agentrt/landingci/",
	// Approvals and what they let run.
	"api/internal/approvals/",
	"api/internal/approvalhome/",
	"api/internal/approvalnotify/",
	"api/internal/actions/",
	"api/internal/actuation/",
	"api/internal/actuationpolicy/",
	"api/internal/releasepolicy/",
	"api/internal/workactions/",
	// The processes that hold the credentials, and their configuration.
	"api/internal/server/",
	"api/internal/agentrt/agentrtapi/",
	"api/internal/config/",
	"api/cmd/platform-api/",
	"api/cmd/operator-plane/",
	"api/cmd/s5check/",
	// The schema the runtime's records live in.
	"api/internal/agentrt/migrate/",
	"api/internal/agentrt/migrations/",
	// The agent host: what an executor runs, where and with which token.
	"api/internal/agentd/",
	"api/cmd/agentd/",
	"api/internal/agentrt/plan/",
	"api/internal/agentrt/infracheck/",
	"api/cmd/infracheck/",
	// Held by the dependency closure of the roots above.
	"api/internal/agentrt/canon/",
	"api/internal/statefile/",
	"api/internal/notice/",
	"api/internal/threadinbox/",
	"api/internal/assessmentview/",
	"api/internal/safego/",
	"api/internal/maintainer/",
	"api/internal/opscontext/",
	"api/internal/localpath/",
	"api/internal/agentrt/actions/",
}

// The roots whose non-test dependency closure must stay inside landingHardGo
// (landing_hard_closure_test.go).
var landingHardRoots = [...]string{
	"api/internal/agentrt/rules/",
	"api/internal/agentrt/landing/",
	"api/internal/agentrt/landinggit/",
	"api/internal/approvals/",
	"api/internal/approvalhome/",
	"api/internal/approvalnotify/",
	"api/internal/actions/",
	"api/internal/actuation/",
	"api/internal/actuationpolicy/",
	"api/internal/releasepolicy/",
	"api/internal/workactions/",
	"api/internal/config/",
	"api/internal/agentrt/migrate/",
	"api/internal/agentrt/infracheck/",
}

// The Console's approval, sign-in and page-loading files.
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
	"console/src/api/landingGrant.ts",
	"console/src/pages/landing-grant/",
	"console/src/pages/shell/progress/LandingAuthorization.tsx",
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
}

var landingHardOtherDirs = [...]string{
	"mcp/platform/",
	".husky/",
	".github/",
}

var landingHardFiles = [...]string{
	"api/go.mod",
	"api/go.sum",
	// scripts/agent/deploy_mac_mini.sh runs its build-operator-plane target
	// with the Mac minis' SSH access and installs what it builds there.
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

// Entries planned by the design, allowed to be absent until their blocks land.
var landingHardFuture = [...]string{
	".github/",
	"console/src/api/landingGrant.ts",
	"console/src/pages/landing-grant/",
}

// The runtime's store is split by file (§12.1): what decides who may do what,
// and what only reads, projects or words it. A name ending in "*" is a prefix.
// A file in neither map, and any subdirectory, is permission.
const landingStoreDir = "api/internal/agentrt/store/"

var landingStorePermission = map[string]string{
	"landing*":                "the landing gate's requests, grants, cards and evidence",
	"pin*":                    "pinning work to a host or executor",
	"nomerge*":                "what keeps a delivery from being merged",
	"rules.go":                "the signed rules in force",
	"rulerows.go":             "the signed rules' rows",
	"envelope.go":             "the standing envelope agents work inside",
	"missiondecide.go":        "the Owner's decisions on missions",
	"proposebounds.go":        "what a proposal may ask for",
	"propose.go":              "proposing missions and tasks",
	"signoff.go":              "the Owner's sign-off",
	"acceptancesignoff.go":    "the Owner's sign-off of an acceptance",
	"controls.go":             "the Owner's switches",
	"cardauto.go":             "cards the runtime answers itself",
	"cardkinds.go":            "which cards exist and who answers them",
	"route.go":                "which executor a task goes to",
	"claim.go":                "a host taking a task",
	"pick.go":                 "which task a claim gets",
	"competence.go":           "what an executor is trusted with",
	"repos.go":                "the repositories the runtime may touch",
	"core.go":                 "writers, actors and their registration",
	"close.go":                "closing missions and tasks",
	"event.go":                "appending events",
	"write.go":                "the write path every record takes",
	"vocab.go":                "the event vocabulary Append accepts",
	"keyed.go":                "idempotent keyed writes",
	"accept.go":               "accepting a submitted implementation through verification and review",
	"adopt.go":                "accepting a salvaged result",
	"alerts.go":               "telling the Owner a record claims an authority it lacks",
	"assess.go":               "dispatching an assessment only once the Owner opts in",
	"assessrequest.go":        "the Owner's opt-in record for an assessment",
	"attempt.go":              "attempts, their leases and what they hand in",
	"benchmark.go":            "benchmark results that feed routing",
	"blockedcard.go":          "the Owner's card on a blocked task and what its answers do",
	"blockedclose.go":         "closing a mission on the Owner's answer",
	"brief.go":                "turning a message to the seat into a mission",
	"briefdue.go":             "the controller's own notes to the seat",
	"claims.go":               "deciding a claim of the Owner's approval by records",
	"claimsubject.go":         "what a check is told of the work it checks",
	"conclusion.go":           "the platform's own conclusions",
	"debtcheck.go":            "a debt's acceptance spec",
	"debtfloor.go":            "the debt numbering floor only the Owner or runtime raises",
	"debtlinks.go":            "whether a debt can start",
	"debts.go":                "debt records and their writers",
	"debtvocab.go":            "the debt event vocabulary",
	"dispatch.go":             "readying proposed tasks",
	"exam.go":                 "the qualifying exam an executor must pass",
	"examgrade.go":            "grading the qualifying exam",
	"examvoid.go":             "voiding a paper, the Owner's only",
	"externalwork.go":         "external runs and the collector's identity",
	"facts.go":                "external facts rules are evaluated against",
	"gateconclude.go":         "whether a gate passed",
	"gateend.go":              "ending a gate batch",
	"gates.go":                "the runtime's gate runs",
	"gatesplan.go":            "plan changes and the gates",
	"goal.go":                 "turning the Owner's goal into a mission",
	"intake.go":               "intake clients, their tokens and limits",
	"intakeupdates.go":        "what an intake client may read",
	"issuebrief.go":           "the person's confirmation of a brief",
	"issuecard.go":            "the Owner's cards on business issues",
	"issuecarddecide.go":      "applying the Owner's answers on business issues",
	"issueclose.go":           "the close predicate of an issue",
	"issuedesign.go":          "a design and its independent review",
	"issueevidence.go":        "the evidence an issue's gates read",
	"issueread.go":            "who may read which issues",
	"issues.go":               "business issues",
	"issuesubmit.go":          "bringing an issue and an intake client's limits",
	"issuetake.go":            "the take predicate that gives an issue work",
	"kernel.go":               "the mission, task and attempt state machine",
	"localpath.go":            "which artifacts are scanned for local paths",
	"messages.go":             "who may send which message kinds",
	"missionbudget.go":        "a mission's budget and the Owner's extra rounds",
	"planactivate.go":         "activating a step under the signed rule",
	"planbindings.go":         "installing the conditions of an adopted plan",
	"planchange.go":           "plan changes within the standing authorization",
	"plandecide.go":           "applying the Owner's decision on a plan change",
	"planfactssnapshot.go":    "the facts a step's done rule reads",
	"planimport.go":           "importing a plan",
	"planimportcard.go":       "the Owner's card on a plan import",
	"planobservations.go":     "the observations a done rests on",
	"planparse.go":            "the program check of a planner's plan",
	"planproposal.go":         "the Owner's card on a plan change",
	"planresult.go":           "recording a plan",
	"planview.go":             "adopting a plan view",
	"pools.go":                "vendor pools and the budget ledger",
	"prune.go":                "deleting attempt branches",
	"recorderimport.go":       "the Owner's card on a recorder import and landing it",
	"retry.go":                "retrying after a failure",
	"scores.go":               "in-job scores that feed routing",
	"seat.go":                 "seats the platform registers",
	"seed.go":                 "the initial capability facts",
	"stepevidence.go":         "a step's own work as evidence for its done rule",
	"stg_block_record.go":     "STG's closed block records",
	"stg_card_answer.go":      "applying the Owner's STG card answers",
	"stg_card_protocol.go":    "STG card identities",
	"stg_cards.go":            "STG's cards under the supervisor's rule",
	"stg_control.go":          "STG's signed control rule",
	"stg_coverage.go":         "STG coverage proofs that settle members",
	"stg_gate.go":             "authorizing an STG release",
	"stg_gate_policy.go":      "the STG gate's policy",
	"stg_gate_recovery.go":    "STG run recovery",
	"stg_landing.go":          "STG's closing snapshots",
	"stg_release.go":          "the STG release ledger",
	"stg_release_proofs.go":   "STG release proofs",
	"stg_release_protocol.go": "the STG release protocol and its writers",
	"stg_release_scopes.go":   "STG's Owner-only ledger edges",
	"stg_release_store.go":    "the STG release stream",
	"stg_sample.go":           "the Tier C sample actions",
	"workspaceref.go":         "keeping a task from naming another task's workspace",
}

// A _test.go file whose name starts with an open file's stem, followed by
// nothing, a digit or "_", is open with it.
var landingStoreOpen = map[string]string{
	"artifactsearch.go":       "lists artifact metadata, reads nothing it could not before",
	"assessaccept.go":         "records an assessment, which is advice only",
	"blockedexcerpt.go":       "what a blocked card quotes",
	"blockedrecommend.go":     "a card's recommended answer, which the Owner still gives",
	"briefwait.go":            "says a brief has no host",
	"claimfailed.go":          "what a retry is told of earlier rounds",
	"eventcursor.go":          "reads events in order",
	"examspec.go":             "renders exam specs from given records",
	"explaincache.go":         "caches read-only explanations",
	"explainroot.go":          "a read-only explanation",
	"hostdisk.go":             "a host's free disk and its alert",
	"ids.go":                  "id spelling",
	"issueshown.go":           "an issue's displayed state",
	"loopalert.go":            "alerts on a failing controller loop",
	"planevidence.go":         "read-only evidence suggestions",
	"plannote.go":             "notes shown beside a row, never its tick",
	"planpace.go":             "a round's pace, a projection",
	"planverificationview.go": "a read-only landing view",
	"planviewrecent.go":       "the recent changes listing",
	"planviewsnapshot.go":     "a read-only plan snapshot",
	"planviewsummary.go":      "a progress summary",
	"preparefailed.go":        "blocks a task whose setup failed; grants nothing",
	"presence.go":             "a host's presence view",
	"presencejudge.go":        "alerts when presence cannot be judged",
	"presencelive.go":         "when a host counts as alive",
	"presencepass.go":         "presence alerts",
	"progress.go":             "the progress projection",
	"progressarch.go":         "the architecture table projection",
	"progresseta.go":          "a mission's expected end",
	"progressgates.go":        "why a gate has not passed, a projection",
	"progressneeds.go":        "what waits on the Owner, a projection",
	"progressnext.go":         "when the Owner is next needed, a projection",
	"progresspersist.go":      "whether an attempt's work is saved, a projection",
	"progressschema.go":       "the platform's own health, a projection",
	"signals.go":              "outbox topic names",
	"snapshot.go":             "what the runtime knows, read for the seat",
	"stepexplain.go":          "what keeps a step from done, read-only",
	"stg_card_copy.go":        "STG card wording",
	"stg_lease.go":            "an STG lease diagnostic that changes no state",
	"waits.go":                "alerts on waits past their limit",
	"withdrawn.go":            "a card for a task no executor may take",
}

// Open paths: a change to one cannot widen what agents may do without the
// Owner, so it needs neither the core approval nor the signed allow list.
var landingOpen = map[string]string{
	"api/internal/agentrt/questioncards/":               "renders question cards; the answers are read by approvals",
	"api/internal/agentrt/stgrelease/":                  "drives STG releases the store's ledger authorizes",
	"api/internal/agentrt/stgpreflight/":                "read-only checks before an STG release",
	"api/internal/agentrt/controller/":                  "schedules the store's passes; each decides for itself",
	"api/internal/agentrt/relay/":                       "carries messages the store checks",
	"api/internal/agentrt/agentrttest/":                 "test helpers",
	"api/internal/agentrt/gaterun/":                     "runs gate commands whose verdict the store judges",
	"api/internal/agentrt/planimport/":                  "parses an import the Owner's card decides",
	"api/internal/agentrt/debtimport/":                  "parses the debt ledger, no writes",
	"api/internal/agentrt/planview/":                    "parses and renders plan views",
	"api/internal/agentrt/recorderimport/":              "parses a recorder log the Owner's card decides",
	"api/internal/agentrt/seatproto/":                   "parses the seat's protocol lines",
	"api/internal/agentrt/exam/":                        "the exam's items; grading is the store's",
	"api/internal/questions/":                           "questions; their answers grant nothing",
	"api/internal/cluster/":                             "the cluster read transport",
	"api/internal/placement/":                           "where workloads run, a read",
	"api/internal/probe/":                               "health probes",
	"api/internal/delivery/":                            "delivery status, a read",
	"api/internal/lineage/":                             "release lineage, a read",
	"api/internal/releases/":                            "the releases listing",
	"api/cmd/agentrt-gate/":                             "a gate runner the store judges",
	"api/cmd/agentrt-plan-import/":                      "submits an import the Owner decides",
	"api/cmd/debtimport/":                               "the debt ledger's importer and exporter",
	"api/cmd/stg-release-probe/":                        "a read-only STG probe",
	"api/Makefile":                                      "developer targets; no credentialed pipeline runs it",
	"console/vitest.config.ts":                          "the Console's test runner",
	"console/tsconfig.json":                             "type checking only",
	"console/tsconfig.node.json":                        "type checking only",
	"console/src/api/issues.ts":                         "sends the operator token only to the issue routes",
	"console/src/api/agentRuntimePlan.ts":               "sends the operator token only to the plan routes",
	"console/src/api/questions.ts":                      "sends the operator token only to the question routes",
	"console/src/pages/shell/releases/ReleasesPage.tsx": "reads whether a token is set",
	"console/src/pages/shell/owner/useOwnerConsole.ts":  "reads whether a token is set",
}

func landingStoreMatch(name string, m map[string]string) string {
	for entry := range m {
		if name == entry || strings.HasSuffix(entry, "*") && strings.HasPrefix(name, strings.TrimSuffix(entry, "*")) {
			return entry
		}
	}
	return ""
}

// landingStoreOpenFile: name is a file directly in the store, open, and not
// matched by a permission entry.
func landingStoreOpenFile(name string) bool {
	if strings.Contains(name, "/") || landingStoreMatch(name, landingStorePermission) != "" {
		return false
	}
	if _, ok := landingStoreOpen[name]; ok {
		return true
	}
	base, ok := strings.CutSuffix(name, "_test.go")
	if !ok {
		return false
	}
	for entry := range landingStoreOpen {
		rest, ok := strings.CutPrefix(base, strings.TrimSuffix(entry, ".go"))
		if ok && (rest == "" || rest[0] == '_' || rest[0] >= '0' && rest[0] <= '9') {
			return true
		}
	}
	return false
}

func inLandingOpen(p string) bool {
	if name, ok := strings.CutPrefix(p, landingStoreDir); ok {
		return landingStoreOpenFile(name)
	}
	for entry := range landingOpen {
		if p == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(p, entry) {
			return true
		}
	}
	return false
}

// LandingOpen: p is a bifrost-platform path on the open list and on no
// permission or absolute entry. LandingSoftRefusals gives it no core,
// floor_soft or presentation refusal, and no rule_paths refusal for being
// outside allow; deny and size still apply.
func LandingOpen(repo, p string) bool {
	return repo == "bifrost-platform" && !LandingHardAbsolute(repo, p) && !LandingHard(repo, p) && inLandingOpen(p)
}

func inLandingEntries(p string, entries []string) bool {
	for _, entry := range entries {
		if p == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(p, entry) {
			return true
		}
	}
	return false
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
	if name, ok := strings.CutPrefix(p, landingStoreDir); ok {
		return !landingStoreOpenFile(name)
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

// LandingHardCore is platform's trusted core. Infra has no core: its
// grantable hard paths are floor-grantable (InfraFloorEntry).
func LandingHardCore(repo, p string) bool {
	if repo == "bifrost-trade-infra" {
		return false
	}
	return !LandingHardAbsolute(repo, p) && LandingHard(repo, p)
}

// LandingHardCoreEntry is the §4.2 entry a hard_core path matches, for the
// core-level approval's material (§12.2 item 2); "" when p is not hard_core.
// Basename and Dockerfile entries name the basename rule they match.
func LandingHardCoreEntry(repo, p string) string {
	if !LandingHardCore(repo, p) {
		return ""
	}
	if name, ok := strings.CutPrefix(p, landingStoreDir); ok {
		if entry := landingStoreMatch(name, landingStorePermission); entry != "" && !strings.Contains(name, "/") {
			return landingStoreDir + entry
		}
		return landingStoreDir
	}
	for _, list := range [][]string{landingHardGo[:], landingHardConsole[:], landingHardOtherDirs[:], landingHardFiles[:]} {
		for _, entry := range list {
			if p == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(p, entry) {
				return entry
			}
		}
	}
	base := path.Base(p)
	if strings.HasPrefix(base, "Dockerfile") {
		return "**/Dockerfile*"
	}
	return "**/" + base
}

// LandingDecisionChain: p is on the Owner's decision chain (§12.2 item 2):
// the process-external approval source (§4.2 B), the Console decision UI and
// its credentials (§4.2 C), or the approvals and actuation packages.
func LandingDecisionChain(repo, p string) bool {
	if !LandingHardCore(repo, p) {
		return false
	}
	return inLandingEntries(p, landingHardConsole[:]) || strings.HasPrefix(p, "mcp/platform/") ||
		strings.HasPrefix(p, "api/internal/approval") || strings.HasPrefix(p, "api/internal/actuation")
}
