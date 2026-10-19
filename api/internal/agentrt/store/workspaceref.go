package store

import (
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// T11 隔离 (v5 §5.3): a task spec or an artifact that refers to another
// task's workspace is refused. A task reads another task's work from its
// artifacts and its pushed branch, never from its directory. agentd keeps an
// attempt's checkout at <root>/tasks/<task>/a<n>, its output beside it at
// a<n>.out, a task's siblings at <root>/tasks/<task>/<repo>, and its root at
// ~/agent-work/agentd (agentd.WorkspaceDir, cmd/agentd). A spec is written
// before its task exists, so whatever workspace it names is another task's.
//
// Once a path's "./" segments and repeated slashes are folded (review 340
// B5), a task id names a workspace where it is a task's directory
// (tasks/<task>) or has something inside it (<task>/a<n>, <task>/<repo>, with
// or without tasks/ before it; review 341 A1). It does not where it is a
// task's record: the id alone, or the platform's route to it
// (/api/v1/agentrt/tasks/<task>, /agentrt/tasks/<task>); nor where it is an attempt's branch, a
// whole ref and nothing after it: agent/<task>/a<n> (refs/heads/agent/…,
// a Gitea branch URL) or agents/<task>/a<n> (refs/remotes/agents/…, where
// agentd fetches a checked task's branches; review 341 A2). A branch is how
// a task is meant to read another's work. agentd writes a check's own
// checkout as "." and its task's directory as ".." in what it hands in, so a
// path beside a checkout (../a<n>.out/tmp) is the task's own. Raw output (log
// tails, checkpoint patches, and a check's verify.json, its commands'
// output) quotes the executor's own paths and is let through.
var taskID = regexp.MustCompile(`t_[0-9A-HJKMNP-TV-Z]{26}`)

var (
	// inside is what follows a task id that has something in its directory.
	inside = regexp.MustCompile(`^/[A-Za-z0-9.]`)
	// branchTail is what follows a task id in an attempt's branch: /a<n>,
	// then the end of the token.
	branchTail = regexp.MustCompile("^/a[0-9]+(?:$|[\\s,;:)\\]}'\"`>]|\\.(?:$|\\s))")
	// apiRoute is the platform's route to a task's record, as a client calls
	// it (/api/v1/agentrt/tasks/<task>) or the docs name it
	// (GET /agentrt/tasks/{id}).
	apiRoute = "agentrt/tasks/"
	// branchPrefixes come before a task id in its attempts' branches
	// (store.AttemptRef, agentd's fetch of a checked task).
	branchPrefixes = []string{"agent/", "agents/"}
)

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
	for _, loc := range taskID.FindAllStringIndex(text, -1) {
		before, after := text[:loc[0]], text[loc[1]:]
		var names bool
		switch {
		case inside.MatchString(after):
			names = !isBranch(before, after)
		case strings.HasSuffix(before, "tasks/"):
			names = !strings.HasSuffix(before, apiRoute)
		}
		if !names {
			continue
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

// isBranch: the task id is an attempt's branch, a whole ref.
func isBranch(before, after string) bool {
	if !branchTail.MatchString(after) {
		return false
	}
	for _, p := range branchPrefixes {
		if strings.HasSuffix(before, p) {
			return true
		}
	}
	return false
}
