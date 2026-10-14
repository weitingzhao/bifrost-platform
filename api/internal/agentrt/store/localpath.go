package store

import "regexp"

// localPath is the closed search rule H8 (DESIGN-host-minimal §H8): a path
// that points into one machine. A drive letter counts only when no letter
// precedes it, so "http://" and "foo:/" do not.
var localPath = regexp.MustCompile(`/Users/|/home/|/private/|/var/folders/|/tmp/|~/|(?:^|[^A-Za-z])[A-Za-z]:[/\\]`)

// ArtifactKinds are the artifact kinds the schema allows (0001_core.sql).
var ArtifactKinds = map[string]bool{"task_spec": true, "mission_spec": true, "report": true, "verify": true, "review": true,
	"log_tail": true, "checkpoint_patch": true, "handover": true, "brief": true, "message": true, "exam": true, "other": true}

// unscannedKinds are the kinds H8 lets through: task specs and briefs (the
// inherited exceptions of H8) and raw output, log tails and patches (v5 §5.2
// 「PUT /artifacts」). Every other kind is scanned, so a kind added later is
// scanned until it is named here (review 156 A2).
var unscannedKinds = map[string]bool{"task_spec": true, "brief": true, "log_tail": true, "checkpoint_patch": true}

// LocalPath reports the first local path in body, unless kind is one H8 lets
// through.
func LocalPath(kind string, body []byte) (string, bool) {
	if unscannedKinds[kind] {
		return "", false
	}
	loc := localPath.FindIndex(body)
	if loc == nil {
		return "", false
	}
	end := loc[1] + 40
	if end > len(body) {
		end = len(body)
	}
	return string(body[loc[0]:end]), true
}
