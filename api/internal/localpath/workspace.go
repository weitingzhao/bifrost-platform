package localpath

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// T11 隔离 (v5 §5.3): a text that names another task's workspace. A task
// reads another task's work from its artifacts and its pushed branch, never
// from its directory. agentd keeps an attempt's checkout at
// <root>/tasks/<task>/a<n>, its output beside it at a<n>.out, a task's
// siblings at <root>/tasks/<task>/<repo> (agentd.WorkspaceDir). The platform
// refuses an artifact that names one (agentrt/store.WorkspaceRef); agentd
// withholds a check's output that still names one after it wrote its own
// directories as "." and "..", as it does for H8 (agentd handIn). One rule,
// used by both.
//
// Once a path's "./" segments and repeated slashes are folded, a task id
// names a workspace where it is a path element: something follows it after
// a slash (<task>/a<n>, <task>/<repo>, <task>/), or a slash comes right before
// it (tasks/<task>, ../<task>, ./<task>). It does not where it is a task's
// record: the id alone, or the platform's route to it (agentrt/tasks/<task>);
// nor where it is an attempt's branch: a whole ref, agent/ or agents/ before
// the id and /a<n> (or a refspec's /*) and the end of the token after it
// (refs/heads/agent/…, refs/remotes/agents/… where agentd fetches a checked
// task's branches), or a Gitea URL at that branch, whatever file of it the
// URL goes on to (…/src/branch/agent/<task>/a<n>/api/go.mod). A path beside a
// checkout (../a<n>.out/tmp) names no task: agentd writes a check's own
// directories that way.
var taskID = regexp.MustCompile(`t_[0-9A-HJKMNP-TV-Z]{26}`)

var (
	// branchTail follows a task id in a whole ref: /a<n> or a refspec's /*,
	// then the end of the token.
	branchTail = regexp.MustCompile("^/(?:a[0-9]+|\\*)(?:$|[\\s,;:)\\]}'\"`>?#]|\\.(?:$|\\s))")
	// urlBranchTail follows a task id in a Gitea URL at an attempt's
	// branch: /a<n>, then the URL's end, its path, query or fragment.
	urlBranchTail = regexp.MustCompile(`^/a[0-9]+(?:$|[/?#\s"'<>)\]])`)
	// apiRoute is the platform's route to a task's record, as a client calls
	// it (/api/v1/agentrt/tasks/<task>) or the docs name it
	// (GET /agentrt/tasks/{id}).
	apiRoute = "agentrt/tasks/"
	// branchPrefixes come before a task id in its attempts' branches
	// (store.AttemptRef, agentd's fetch of a checked task).
	branchPrefixes = []string{"agent/", "agents/"}

	dotSegments = regexp.MustCompile(`/(?:\./)+`)
	slashRuns   = regexp.MustCompile(`/{2,}`)
)

// WorkspaceRef reports the first reference to another task's workspace in
// body, with some text around it, local paths masked. A JSON body is read
// by its decoded keys and strings only, so an escape neither hides a
// reference (\/) nor makes one of a branch (a ref before "\n").
func WorkspaceRef(body []byte) (string, bool) {
	text := string(body)
	if strs, isJSON := JSONStrings(body); isJSON {
		text = strs
	}
	text = foldPaths(text)
	for _, loc := range taskID.FindAllStringIndex(text, -1) {
		before, after := text[:loc[0]], text[loc[1]:]
		var names bool
		switch {
		case strings.HasPrefix(after, "/"):
			names = !isBranch(before, after)
		case strings.HasSuffix(before, "/"):
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
		return Mask(strings.ToValidUTF8(text[start:end], "")), true
	}
	return "", false
}

// isBranch: the task id is an attempt's branch.
func isBranch(before, after string) bool {
	if inBranchURL(before) && urlBranchTail.MatchString(after) {
		return true
	}
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

// inBranchURL: what comes before the task id is a URL's, at a branch the
// Gitea way (http(s)://…/branch/agent/<task>; folded, the scheme's slashes are
// one).
func inBranchURL(before string) bool {
	if !strings.HasSuffix(before, "/branch/agent/") {
		return false
	}
	token := before[strings.LastIndexAny(before, " \t\r\n\"'`<>()[]{}")+1:]
	return strings.HasPrefix(token, "https:/") || strings.HasPrefix(token, "http:/")
}

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

// JSONStrings is every key and string value of a JSON body, one per line,
// and whether body is JSON at all.
func JSONStrings(body []byte) (string, bool) {
	if !json.Valid(body) {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return "", false
	}
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			b.WriteString(x)
			b.WriteByte('\n')
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys) // the same text, and the same first match, every time
			for _, k := range keys {
				b.WriteString(k)
				b.WriteByte('\n')
				walk(x[k])
			}
		}
	}
	walk(v)
	return b.String(), true
}
