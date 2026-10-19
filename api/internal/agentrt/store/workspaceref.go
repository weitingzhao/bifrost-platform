package store

import "github.com/weitingzhao/bifrost-platform/api/internal/localpath"

// unrefKinds are the kinds the workspace rule lets through: raw output and
// patches (log tails, checkpoint patches), which quote the executor's own
// paths. A check's verify.json is scanned: agentd withholds its outputs when
// they still name another task's workspace (review 342 A3).
var unrefKinds = map[string]bool{"log_tail": true, "checkpoint_patch": true}

// WorkspaceRef reports the first reference to another task's workspace in
// body (T11 隔离, v5 §5.3; the rule is localpath.WorkspaceRef, shared with
// agentd), unless kind is one the rule lets through.
func WorkspaceRef(kind string, body []byte) (string, bool) {
	if unrefKinds[kind] {
		return "", false
	}
	return localpath.WorkspaceRef(body)
}
