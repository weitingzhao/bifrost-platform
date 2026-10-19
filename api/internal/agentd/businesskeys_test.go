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
		a := &Attempt{Claim: &Claimed{Role: "implement", EffectClass: "branch"}, Spec: TaskSpec{Text: "# do it", Verify: [][]string{{"go", "test", "./..."}}},
			Dir: t.TempDir(), Out: t.TempDir()}
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
