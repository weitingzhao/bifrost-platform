package agentd

import (
	"fmt"
	"slices"
	"strings"
)

// T13 无业务键 (v5 §5.3): the names no executor's environment holds. An
// executor's environment is rebuilt from an allow list (AllowedEnv) plus its
// own login and configuration (envKeys); this deny list is the check after
// them, on each spawn the executors build (keyless), so a builder that let
// one more name through fails the attempt instead of handing it a key: a
// kubeconfig, a database or Redis address, an IB, cloud, Gitea, GitHub,
// platform or data vendor credential, agentd's own token files, git's
// credential settings (agentd's git gets Gitea's through GIT_CONFIG_*), an
// SSH or Docker socket.
var (
	businessKeyNames    = []string{"KUBECONFIG", "DATABASE_URL", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "GH_TOKEN"}
	businessKeyPrefixes = []string{"PG", "REDIS", "IB_", "AWS_", "AZURE_", "GOOGLE_", "GCLOUD", "KUBE", "GIT_", "GITEA", "GITHUB",
		"BIFROST", "AGENTRT", "AGENTD", "PLATFORM_", "MASSIVE", "POLYGON", "FLEX", "ARGOCD", "VAULT", "NTFY", "SSH_", "DOCKER_"}
	businessKeySuffixes = []string{"_TOKEN", "_SECRET", "_PASSWORD", "_PASSWD", "_API_KEY", "_PRIVATE_KEY", "_DSN", "_CREDENTIALS", "_KUBECONFIG"}
)

// BusinessKey says whether an environment variable's name is on the deny
// list, whatever its case.
func BusinessKey(name string) bool {
	n := strings.ToUpper(name)
	if slices.Contains(businessKeyNames, n) {
		return true
	}
	for _, p := range businessKeyPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	for _, s := range businessKeySuffixes {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}

// ownLogins are the denied names an executor still gets: its own login,
// without which it does not run (claude's token, cursor's key; codex logs in
// from the auth.json in its own home).
var ownLogins = map[string][]string{
	"exec:claude": {"CLAUDE_CODE_OAUTH_TOKEN"},
	"exec:cursor": {"CURSOR_API_KEY"},
}

// noBusinessKeys refuses an executor's environment that holds a name on the
// deny list other than the executor's own login.
func noBusinessKeys(executor string, env []string) error {
	var found []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if BusinessKey(k) && !slices.Contains(ownLogins[executor], k) {
			found = append(found, k)
		}
	}
	if len(found) > 0 {
		slices.Sort(found)
		return fmt.Errorf("agentd: %s's environment holds what no task may see: %s", executor, strings.Join(slices.Compact(found), ", "))
	}
	return nil
}
