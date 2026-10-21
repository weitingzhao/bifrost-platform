package agentd

import (
	"strings"
	"testing"
)

// hostKeys are names a host's environment may hold that no task may see.
var hostKeys = []string{"KUBECONFIG", "PGPASSWORD", "PGHOST", "DATABASE_URL", "REDIS_URL", "IB_ACCOUNT", "AWS_SECRET_ACCESS_KEY",
	"GIT_CONFIG_KEY_0", "GIT_ASKPASS", "GITEA_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "AGENTD_TOKEN_FILE", "AGENTD_CLAUDE_TOKEN_FILE",
	"AGENTRT_DSN", "BIFROST_ADMIN_TOKEN", "PLATFORM_REPORTER_TOKEN", "MASSIVE_API_KEY", "POLYGON_API_KEY", "SSH_AUTH_SOCK",
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "some_admin_token", "DOCKER_HOST", "NTFY_TOKEN", "ARGOCD_AUTH_TOKEN", "X_PRIVATE_KEY"}

// T13: the deny list holds the keys a host has, and none of what an
// executor is meant to see but its own login.
func TestBusinessKeys(t *testing.T) {
	for _, k := range hostKeys {
		if !BusinessKey(k) {
			t.Errorf("%s is not on the deny list", k)
		}
	}
	for k := range allowedEnv {
		if BusinessKey(k) {
			t.Errorf("the allowed %s is on the deny list", k)
		}
	}
	for executor, keys := range envKeys {
		for _, k := range keys {
			own := false
			for _, o := range ownLogins[executor] {
				own = own || o == k
			}
			if BusinessKey(k) != own {
				t.Errorf("%s's %s: on the deny list %v, its own login %v", executor, k, BusinessKey(k), own)
			}
		}
	}
	if err := noBusinessKeys("exec:codex", []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=t"}); err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Errorf("codex with claude's token: %v", err)
	}
	if err := noBusinessKeys("exec:claude", []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=t"}); err != nil {
		t.Errorf("claude with its own token: %v", err)
	}
}

// T13 无业务键: whatever the host's environment holds, every executor's
// environment and the deny list have no name in common but the executor's
// own login.
func TestNoExecutorSeesABusinessKey(t *testing.T) {
	environ := []string{"PATH=/usr/bin:/bin", "HOME=/home/agent", "LANG=C.UTF-8"}
	for _, k := range hostKeys {
		environ = append(environ, k+"=from-host")
	}
	ex := Executors("/usr/local/bin/agentd", environ, Secrets{CursorAPIKey: "k1", ClaudeOAuthToken: "t1"})
	if len(ex) != 4 {
		t.Fatalf("%d executors", len(ex))
	}
	for name, e := range ex {
		// Walled: an implementer's claude starts only inside the wall.
		a := &Attempt{Claim: &Claimed{Role: "implement", EffectClass: "branch"}, Spec: TaskSpec{Text: "# do it", Verify: [][]string{{"go", "test", "./..."}}},
			Dir: t.TempDir(), Out: t.TempDir(), Walled: true}
		sp, err := e(a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := noBusinessKeys(name, sp.Env); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		for _, kv := range sp.Env {
			if strings.HasSuffix(kv, "=from-host") {
				t.Errorf("%s got the host's %s", name, kv)
			}
		}
	}
}

// Review 489 A2: behind the test database stage the executor has three names
// the deny list holds (AGENTRT_TEST_*), set by agentd's own stage and naming
// only the attempt's own sockets: the one exception. Any other denied name,
// one of the three pointing elsewhere, or another executor's login is refused.
func TestStageKeyless(t *testing.T) {
	sock, other := "/home/a/tasks/t_x/a1.out/tmp/db", "/home/a/tasks/t_y/a1.out/tmp/db"
	base := []string{"HOME=/home/a/tasks/t_x/a1.out/home", "PATH=/usr/bin:/bin", "TMPDIR=/home/a/tasks/t_x/a1.out/tmp"}
	if err := stageKeyless("exec:codex", stageEnv(append([]string(nil), base...), sock, "agent"), sock, "agent"); err != nil {
		t.Fatalf("the stage's own three: %v", err)
	}
	got := stageEnv(append([]string(nil), base...), sock, "agent")
	for _, k := range []string{"AGENTRT_TEST_PG", "AGENTRT_TEST_REDIS", "AGENTRT_TEST_REQUIRED"} {
		if len(envValues(got, k)) != 1 {
			t.Fatalf("%s is in the stage's environment %d times: %v", k, len(envValues(got, k)), got)
		}
	}
	claude := append(append([]string(nil), base...), "CLAUDE_CODE_OAUTH_TOKEN=x")
	if err := stageKeyless("exec:claude", stageEnv(claude, sock, "agent"), sock, "agent"); err != nil {
		t.Fatalf("claude's own login: %v", err)
	}
	for name, env := range map[string][]string{
		"another task's database": stageEnv(append([]string(nil), base...), other, "agent"),
		"another host's database": append(append([]string(nil), base...), "AGENTRT_TEST_PG=host=192.168.10.73 port=30432 user=postgres",
			"AGENTRT_TEST_REDIS=unix://"+sock+"/redis.sock", "AGENTRT_TEST_REQUIRED=1"),
		"a kubeconfig":             stageEnv(append(append([]string(nil), base...), "KUBECONFIG=/home/a/.kube/config"), sock, "agent"),
		"another user":             stageEnv(append([]string(nil), base...), sock, "postgres"),
		"another executor's login": stageEnv(append([]string(nil), claude...), sock, "agent"),
	} {
		executor := "exec:claude"
		if name == "another executor's login" {
			executor = "exec:codex"
		}
		if err := stageKeyless(executor, env, sock, "agent"); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}
