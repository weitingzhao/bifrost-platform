package store

import "regexp"

// localPath is the closed search rule H8 (DESIGN-host-minimal §H8): a path
// that points into one machine. A drive letter counts only when no letter
// precedes it, so "http://" and "foo:/" do not.
var localPath = regexp.MustCompile(`/Users/|/home/|/private/|/var/folders/|/tmp/|~/|(?:^|[^A-Za-z])[A-Za-z]:[/\\]`)

// scannedKinds are the artifact kinds that are inputs to a later step or a
// record for people: a local path in them is refused. Log tails and patches
// are raw output and are not scanned (v5 §5.2 「PUT /artifacts」).
var scannedKinds = map[string]bool{"report": true, "verify": true, "review": true, "handover": true, "other": true}

// LocalPath reports the first local path in body, when kind is scanned.
func LocalPath(kind string, body []byte) (string, bool) {
	if !scannedKinds[kind] {
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
