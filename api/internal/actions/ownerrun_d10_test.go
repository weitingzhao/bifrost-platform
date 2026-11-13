package actions_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// A few entries in the shape of the signed no_d10_paths rule.
var d10Patterns = []string{
	"scripts/run_daemon.py",
	"src/bifrost_worker/daemon/execution/**",
	"k8s/overlays/stg/daemon-scale-zero.patch.yaml",
	"k8s/base/worker/**",
	"config/ops-context.yaml",
}

func withD10(t *testing.T, d actions.D10Paths) {
	t.Helper()
	actions.SetD10Paths(func(context.Context) actions.D10Paths { return d })
	t.Cleanup(func() { actions.SetD10Paths(nil) })
}

func ownerRunTier(t *testing.T, command string) actions.Tier {
	t.Helper()
	act, ok := actions.ByID("owner_run_command")
	if !ok {
		t.Fatal("owner_run_command is not in the catalog")
	}
	return act.TierOf(context.Background(), map[string]any{"command": command})
}

// The acceptance case "a trade-related command is refused" is this test: it
// exercises the classifier only and sends nothing to any trade system.
func TestOwnerRunNamingAD10PathIsTierXWhileLocked(t *testing.T) {
	withD10(t, actions.D10Paths{Locked: true, Known: true, Patterns: d10Patterns, Match: releasepolicy.PathMatches})
	for _, command := range []string{
		"python scripts/run_daemon.py --live",
		"cd /srv/bifrost-trade-worker && git checkout HEAD~1 -- src/bifrost_worker/daemon/execution/orders.py",
		"kubectl apply -f bifrost-trade-infra/k8s/overlays/stg/daemon-scale-zero.patch.yaml",
		"kubectl delete -k ./k8s/base/worker/",
		"rm -rf k8s/overlays/stg",                                // a directory above a D10 file
		`sed -i 's/BLOCKED/UNLOCKED/' "config/ops-context.yaml"`, // quoted
		"cat<k8s/base/worker/deployment.yaml|kubectl apply -f -", // no spaces
	} {
		if got := ownerRunTier(t, command); got != actions.TierX {
			t.Errorf("%q: tier %s, want X", command, got)
		}
	}
}

func TestOwnerRunOrdinaryCommandsStayTierD(t *testing.T) {
	withD10(t, actions.D10Paths{Locked: true, Known: true, Patterns: d10Patterns, Match: releasepolicy.PathMatches})
	for _, command := range []string{
		"echo hello",
		"kubectl -n cicd get pipelineruns",
		"kubectl -n cicd create -f - <<'EOF'\napiVersion: tekton.dev/v1\nkind: PipelineRun\nEOF",
		"git -C /srv/bifrost-platform log --oneline -3 -- api/internal/actions",
		"ls k8s", // one segment: not a directory "above" in any useful sense
		"python3 scripts/release/release_policy.py --help",
	} {
		if got := ownerRunTier(t, command); got != actions.TierD {
			t.Errorf("%q: tier %s, want D", command, got)
		}
	}
}

func TestOwnerRunFailsClosedWhenThePolicyCannotBeRead(t *testing.T) {
	withD10(t, actions.D10Paths{Locked: true, Known: false, Match: releasepolicy.PathMatches})
	if got := ownerRunTier(t, "echo hello"); got != actions.TierX {
		t.Fatalf("unknown paths while locked: tier %s, want X", got)
	}
	withD10(t, actions.D10Paths{Locked: true, Known: true, Patterns: d10Patterns})
	if got := ownerRunTier(t, "echo hello"); got != actions.TierX {
		t.Fatalf("no matcher while locked: tier %s, want X", got)
	}
}

// One switch: when the spine says D10 is unlocked, this layer stands down.
func TestOwnerRunFollowsTheSpineSwitch(t *testing.T) {
	withD10(t, actions.D10Paths{Locked: false, Known: true, Patterns: d10Patterns, Match: releasepolicy.PathMatches})
	if got := ownerRunTier(t, "python scripts/run_daemon.py"); got != actions.TierD {
		t.Fatalf("unlocked: tier %s, want D", got)
	}
}

func TestOwnerRunWithoutTheLookupIsTierD(t *testing.T) {
	actions.SetD10Paths(nil)
	if got := ownerRunTier(t, "python scripts/run_daemon.py"); got != actions.TierD {
		t.Fatalf("no lookup installed: tier %s, want D", got)
	}
}
