package store

import (
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// T11 隔离 (v5 §5.3): a task spec or an artifact that refers to a task's
// workspace is refused. A task reads another task's work from its
// artifacts and its pushed branch, never from its directory. agentd keeps an
// attempt's checkout at <root>/tasks/<task>/a<n>, its output beside it at
// a<n>.out, a task's siblings at <root>/tasks/<task>/<repo>, and its root at
// ~/agent-work/agentd (agentd.WorkspaceDir, cmd/agentd). A spec is written
// before its task exists, so whatever workspace it names is another task's.
//
// What names one, once a path's "./" segments and repeated slashes are
// folded (review 340 B5): agentd's tasks directory with a task in it, and
// something inside a task's directory (<task>/a<n>, <task>/<repo>), with or
// without the tasks/ before it. What does not: a task named by its id alone
// or as the API names it (tasks/<task>), agentd's root alone, an attempt's
// branch (agent/<task>/a<n>, refs/heads/agent/<task>/a<n>), which is how a
// task is meant to read another's work (review 340 A1), and a path beside a
// checkout (../a<n>.out/tmp): agentd writes a check's own checkout as "." and
// its task's directory as ".." in what it hands in, so that is the task's
// own. Raw output (log tails, checkpoint patches, and a check's verify.json,
// which is its commands' output) quotes the executor's own paths and is let
// through.
var workspaceRef = regexp.MustCompile(`agentd/tasks/t_|t_[0-9A-HJKMNP-TV-Z]{26}/[A-Za-z0-9]`)

// branchPrefix is what comes before a task id in an attempt's branch
// (store.AttemptRef).
const branchPrefix = "agent/"

var (
	dotSegments = regexp.MustCompile(`/(?:\./)+`)
	slashRuns   = regexp.MustCompile(`/{2,}`)
)

// foldPaths folds "./" segments and repeated slashes, without touching the
// file system.
func foldPaths(s string) string {
	for {
		next := slashRuns.ReplaceAllString(dotSegments.ReplaceAllString(s, "/"), "/")
		if next == s {
			return s
		}
		s = next
	}
}

// unrefKinds are the kinds the workspace rule lets through.
var unrefKinds = map[string]bool{"log_tail": true, "checkpoint_patch": true, "verify": true}

// WorkspaceRef reports the first reference to a task's workspace in body, a
// local path in it masked, unless kind is one the rule lets through. A JSON
// body is read by its decoded strings too, so an escape (\/) hides nothing.
func WorkspaceRef(kind string, body []byte) (string, bool) {
	if unrefKinds[kind] {
		return "", false
	}
	for _, text := range []string{string(body), jsonStrings(body)} {
		if ref, found := workspaceRefIn(foldPaths(text)); found {
			return ref, true
		}
	}
	return "", false
}

func workspaceRefIn(text string) (string, bool) {
	for _, loc := range workspaceRef.FindAllStringIndex(text, -1) {
		if strings.HasPrefix(text[loc[0]:], "t_") && strings.HasSuffix(text[:loc[0]], branchPrefix) {
			continue // an attempt's branch
		}
		start, end := loc[0]-40, loc[1]+20
		if start < 0 {
			start = 0
		}
		if end > len(text) {
			end = len(text)
		}
		return localpath.Mask(strings.ToValidUTF8(text[start:end], "")), true
	}
	return "", false
}
