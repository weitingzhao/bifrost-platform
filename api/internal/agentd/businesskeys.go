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

// T13 at the last spawn there is (review 489 A2): an executor behind the
// test database stage (testdb.go) gets, on top of its spawn's environment
// (keyless), the stage's three names: AGENTRT_TEST_PG, AGENTRT_TEST_REDIS,
// AGENTRT_TEST_REQUIRED, which the AGENTRT prefix denies. They are the one
// exception: set by agentd's own stage, never by a builder (keyless refuses
// them in any spawn) nor taken from the host, and naming only the attempt's
// own throwaway Postgres and Redis, on sockets in its own directory.
// stageEnv is the environment the executor then has; stageKeyless refuses
// it unless every denied name in it is the executor's own login or one of
// the three with the value the stage gives it for this attempt.

// stageEnv is the environment the test database stage gives the executor
// whose spawn has env, on the sockets in sock, as user.
func stageEnv(env []string, sock, user string) []string {
	for _, kv := range testDBEnv(sock, user) {
		env = withEnv(env, kv[0], kv[1])
	}
	return env
}

// stageKeyless is noBusinessKeys for an executor behind the test database
// stage over sock, as user.
func stageKeyless(executor string, env []string, sock, user string) error {
	own := map[string]string{}
	for _, kv := range testDBEnv(sock, user) {
		own[kv[0]] = kv[1]
	}
	rest := make([]string, 0, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if want, ok := own[k]; ok {
			if v != want {
				return fmt.Errorf("agentd: %s's %s is %q, not this attempt's own test database's", executor, k, v)
			}
			continue
		}
		rest = append(rest, kv)
	}
	return noBusinessKeys(executor, rest)
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
