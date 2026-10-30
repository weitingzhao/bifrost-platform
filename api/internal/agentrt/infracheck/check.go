// Package infracheck owns the infra quick check and its pinned Python setup.
package infracheck

import (
	"path"
	"slices"
	"strings"
)

// infraVenv is where an infra task's Python environment is made, relative to
// the checkout.
const infraVenv = ".git/venv"

// infraPackages are what the infra tests import beyond the standard library,
// pytest's own dependencies included: each pinned, installed as a wheel and
// nothing else, so no release published later changes a check.
var infraPackages = []string{"pytest==9.0.2", "pluggy==1.6.0", "iniconfig==2.1.0", "packaging==25.0", "pygments==2.19.2",
	"pyyaml==6.0.3"}

// Check is the infra check's script. It refuses missing test list entries,
// then refuses a tree in which a
// test it runs would sit inside a Python package: pytest imports a package's
// __init__.py, whose pytest_plugins can make it a plugin that sets the run's
// outcome, and neither -c, --noconftest nor the autoload switch stops that
// (review 331 A1). infra has no package; a change that makes one there is
// refused, the file named. Then it runs pytest, as the comment on Repos says.
func Check() string {
	var inits, trees []string
	seen := map[string]bool{}
	for _, p := range infraTests {
		if !strings.HasSuffix(p, ".py") {
			trees = append(trees, p)
		}
		for d := path.Dir(p); ; d = path.Dir(d) {
			if f := path.Join(d, "__init__.py"); !seen[f] {
				seen[f] = true
				inits = append(inits, f)
			}
			if d == "." {
				break
			}
		}
	}
	slices.Sort(inits)
	refuse := `echo "$f: the infra check runs no test inside a Python package (its __init__.py can load a pytest plugin)" >&2; exit 3`
	return `for f in ` + strings.Join(infraTests, " ") + `; do if [ ! -e "$f" ]; then echo "$f: quick list entry missing" >&2; exit 3; fi; done; ` +
		`for f in ` + strings.Join(inits, " ") + `; do if [ -e "$f" ] || [ -L "$f" ]; then ` + refuse + `; fi; done; ` +
		`f=$(find -L ` + strings.Join(trees, " ") + ` -name __init__.py -print 2>/dev/null | head -n 1); if [ -n "$f" ]; then ` + refuse + `; fi; ` +
		`PATH="$PWD/` + infraVenv + `/bin:$PATH" GOCACHE="${TMPDIR:-$HOME/.cache}/go-build" PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 ` +
		`exec python3 -I -m pytest -c /dev/null --rootdir="$PWD" --noconftest -q -p no:cacheprovider ` + strings.Join(infraTests, " ")
}

// infraTests are the infra tests the checks run, under two minutes on
// 2026-10-19's main: those that pass with the repository alone and no network
// at all, not even loopback (an executor runs them in codex's sandbox before
// it hands in), on the agent host (git, bash, python3 and Go; no kubectl), and
// with no conftest.py (there is none among them).
// Left out for time, at about four minutes each and growing (the release
// suite was one minute a week before): scripts/release, scripts/gitea and
// k8s/cicd/gitea-backup. Left out as they serve on loopback:
// scripts/cicd and scripts/agent-host/test_probe_gitea_protection.py. Left
// out as they cannot pass there: agent-config/scripts/recorder (one test runs
// scripts/release three times, three need bifrost-platform beside the
// checkout); scripts/agent-host/test_agent_06_owner.py (the Owner's Mac
// script, with macOS's shasum); scripts/test_agentrt_infra.py (kubectl);
// scripts/test_audit_retention_archive.py and scripts/test_iv_gap_audit.py
// (pyarrow, bifrost_research). scripts/test_access_checks.py joined once its
// fake answered the Application read (TD-378, infra d989dde).
var infraTests = []string{
	"agent-config/scripts/test_check_hardcoded_paths.py",
	"k8s/data/role-matrix",
	"scripts/agentrt",
	"scripts/benchmark",
	"scripts/k3s",
	"scripts/landing",
	"scripts/test_access_checks.py",
	"scripts/test_check_monitoring_persistence.py",
	"scripts/test_check_registry_images.py",
	"scripts/test_iv_depth_plan.py",
	"scripts/test_iv_empty_audit.py",
	"scripts/test_iv_repair_actions.py",
	"scripts/test_precommit_monitoring_checks.py",
	"scripts/test_research_alert_delivery_route.py",
	"scripts/test_research_secret_holders.py",
	"scripts/test_scrub_config_secrets.py",
}
