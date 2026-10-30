package rules

import (
	"path"
	"slices"
	"strings"
)

// Coordinator v3 §4.2 / §12.1. Prefixes protect entire packages, including
// future files, subpackages, tests, fixtures and build-tagged sources.
var landingHardGo = [...]string{
	"api/internal/approvals/",
	"api/internal/approvalhome/",
	"api/internal/approvalnotify/",
	"api/internal/actions/",
	"api/internal/actuation/",
	"api/internal/actuationpolicy/",
	"api/internal/agentrt/store/",
	"api/internal/agentrt/questioncards/",
	"api/internal/agentrt/landing/",
	"api/internal/agentrt/landinggit/",
	"api/internal/agentrt/landingci/",
	"api/internal/agentrt/stgrelease/",
	"api/internal/agentrt/stgpreflight/",
	"api/internal/agentrt/infracheck/",
	"api/internal/agentrt/rules/",
	"api/internal/agentrt/controller/",
	"api/internal/agentrt/agentrtapi/",
	"api/internal/agentrt/relay/",
	"api/internal/agentrt/canon/",
	"api/internal/agentrt/migrate/",
	"api/internal/agentrt/migrations/",
	"api/internal/agentrt/agentrttest/",
	"api/internal/agentd/",
	"api/internal/server/",
	"api/internal/agentrt/actions/",
	"api/internal/agentrt/gaterun/",
	"api/internal/agentrt/plan/",
	"api/internal/agentrt/planimport/",
	"api/internal/agentrt/planview/",
	"api/internal/agentrt/recorderimport/",
	"api/internal/agentrt/seatproto/",
	"api/internal/statefile/",
	"api/internal/notice/",
	"api/internal/safego/",
	"api/internal/localpath/",
	"api/internal/questions/",
	"api/internal/threadinbox/",
	"api/internal/releasepolicy/",
	"api/internal/maintainer/",
	// Runtime transports/configuration and injected evidence/policy providers
	// are trusted dependencies, even when server supplies them via callbacks.
	"api/internal/cluster/",
	"api/internal/config/",
	"api/internal/opscontext/",
	"api/internal/placement/",
	"api/internal/probe/",
	"api/internal/delivery/",
	"api/internal/lineage/",
	"api/internal/releases/",
	"api/internal/agentrt/exam/",
	"api/internal/workactions/",
	"api/cmd/",
}

var landingHardRoots = [...]string{
	"api/internal/approvals/",
	"api/internal/approvalhome/",
	"api/internal/approvalnotify/",
	"api/internal/actions/",
	"api/internal/actuation/",
	"api/internal/actuationpolicy/",
	"api/internal/agentrt/store/",
	"api/internal/agentrt/landing/",
	"api/internal/agentrt/landinggit/",
	"api/internal/agentrt/landingci/",
	"api/internal/agentrt/stgrelease/",
	"api/internal/agentrt/infracheck/",
	"api/internal/agentrt/rules/",
	"api/internal/agentrt/controller/",
	"api/internal/agentrt/agentrtapi/",
	"api/internal/agentrt/relay/",
	"api/internal/agentrt/canon/",
	"api/internal/agentrt/migrate/",
	"api/internal/agentrt/migrations/",
	"api/internal/agentrt/agentrttest/",
	"api/internal/agentd/",
	"api/internal/server/",
	"api/cmd/",
}

var landingHardConsole = [...]string{
	"console/package.json",
	"console/package-lock.json",
	"console/vite.config.ts",
	"console/vitest.config.ts",
	"console/tsconfig.json",
	"console/tsconfig.node.json",
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
	"console/src/pages/shell/needs-you/ApprovalRow.tsx",
	"console/src/pages/landing-grant/",
	"console/src/api/issues.ts",
	"console/src/api/agentRuntimePlan.ts",
	"console/src/api/questions.ts",
	"console/src/api/cluster.ts",
	"console/src/components/shell/RequestActionButton.tsx",
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
	"api/Makefile",
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
	return inLandingEntries(p, landingHardGo[:]) || inLandingEntries(p, landingHardConsole[:]) ||
		inLandingEntries(p, landingHardOtherDirs[:]) || inLandingEntries(p, landingHardFiles[:]) ||
		slices.Contains(landingHardBasenames[:], path.Base(p)) || strings.HasPrefix(path.Base(p), "Dockerfile")
}

func LandingHardAbsolute(repo, p string) bool {
	if repo == "bifrost-trade-infra" {
		return !landingPath(p) || infraHard(p)
	}
	return repo != "bifrost-platform" || !landingPath(p) || strings.HasPrefix(path.Base(p), ".env") ||
		path.Base(p) == ".gitmodules" || strings.HasPrefix(p, ".github/")
}

func LandingHardCore(repo, p string) bool {
	return !LandingHardAbsolute(repo, p) && LandingHard(repo, p)
}

// LandingHardCoreEntry is the §4.2 entry a hard_core path matches, for the
// core-level approval's material (§12.2 item 2); "" when p is not hard_core.
// Basename and Dockerfile entries name the basename rule they match.
func LandingHardCoreEntry(repo, p string) string {
	if !LandingHardCore(repo, p) {
		return ""
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
