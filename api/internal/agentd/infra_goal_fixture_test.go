package agentd_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func infraGoalRemote(t *testing.T, env []string) (string, string) {
	t.Helper()
	bare, _ := bareRemote(t, env)
	// infra's main moves on, so the two mains are different commits.
	seed := filepath.Join(t.TempDir(), "seed")
	sh(t, t.TempDir(), env, "git", "clone", "--quiet", bare, seed)
	must(t, os.WriteFile(filepath.Join(seed, "INFRA.md"), []byte("# infra\n"), 0o600))
	// Model the real quick-test layout. Track a test in each directory so it
	// survives cloning; __init__.py would turn it into a forbidden package.
	// These are fixture files, not a second runtime check list. The offline
	// test below runs Repos' check unchanged to detect an incomplete fixture.
	files := []string{
		"agent-config/scripts/test_check_hardcoded_paths.py",
		"k8s/data/role-matrix/test_check_role_matrix.py",
		"scripts/agentrt/test_t26_probe.py",
		"scripts/benchmark/test_benchmark.py",
		"scripts/k3s/test_rolling_reboot_plan.py",
		"scripts/landing/test_ci_infra_pins.py",
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
	for _, name := range files {
		file := filepath.Join(seed, name)
		must(t, os.MkdirAll(filepath.Dir(file), 0o700))
		must(t, os.WriteFile(file, []byte("def test_fixture():\n    pass\n"), 0o600))
	}
	sh(t, seed, env, append([]string{"git", "add", "INFRA.md"}, files...)...)
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "infra")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, sh(t, seed, env, "git", "rev-parse", "HEAD")
}

// Exercise the PG goal test's committed fixture through the real setup and
// quick script even when no database is available. Only Python is faked.
func TestInfraGoalFixtureCheck(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "python.log")
	must(t, os.WriteFile(filepath.Join(bin, "python3"), []byte(strings.Replace(fakePython, "LOG", log, 1)), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := gitEnvT(t.TempDir())
	bare, base := infraGoalRemote(t, env)
	dir := filepath.Join(t.TempDir(), "infra")
	sh(t, t.TempDir(), env, "git", "clone", "--quiet", bare, dir)
	repo := store.Repos["bifrost-trade-infra"]
	if v := agentd.Verify(context.Background(), dir, env, repo.Setup, time.Minute); !v.Pass {
		t.Fatalf("fixture setup: %+v", v)
	}
	if v := agentd.Verify(context.Background(), dir, env, repo.Verify, time.Minute); !v.Pass {
		t.Fatalf("fixture check: %+v", v)
	}
	raw, err := os.ReadFile(log)
	must(t, err)
	if !strings.Contains(string(raw), "|"+base+"|venv|") {
		t.Fatalf("pytest did not run from the fixture commit's venv: %s", raw)
	}
}
