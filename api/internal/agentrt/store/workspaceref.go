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
// The root alone names no workspace, and is let through.
//
// A task named by its id alone (t_…) is a reference to its record, and is
// let through; so are raw output and patches (log tails, checkpoint patches),
// which quote the executor's own paths.
var workspaceRef = regexp.MustCompile(`tasks/t_[0-9A-HJKMNP-TV-Z]{26}|t_[0-9A-HJKMNP-TV-Z]{26}/a[0-9]+|(?:^|[^A-Za-z0-9_.-])\.\./a[0-9]+(?:\.out)?(?:[^A-Za-z0-9_.-]|$)`)

// unrefKinds are the kinds the workspace rule lets through.
var unrefKinds = map[string]bool{"log_tail": true, "checkpoint_patch": true}

// WorkspaceRef reports the first reference to a task's workspace in body, a
// local path in it masked, unless kind is one the rule lets through.
func WorkspaceRef(kind string, body []byte) (string, bool) {
	if unrefKinds[kind] {
		return "", false
	}
	loc := workspaceRef.FindIndex(body)
	if loc == nil {
		// A JSON body by its decoded strings too, so an escape (\/) hides
		// nothing.
		if body = []byte(jsonStrings(body)); len(body) > 0 {
			loc = workspaceRef.FindIndex(body)
		}
		if loc == nil {
			return "", false
		}
	}
	start, end := loc[0]-40, loc[1]+20
	if start < 0 {
		start = 0
	}
	if end > len(body) {
		end = len(body)
	}
	return localpath.Mask(strings.ToValidUTF8(string(body[start:end]), "")), true
}
