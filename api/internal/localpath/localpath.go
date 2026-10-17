// Package localpath is the closed search rule H8 (DESIGN-host-minimal §H8):
// a path that points into one machine. The platform refuses artifacts that
// contain one (agentrt/store); agentd masks them in the output it hands in as
// verify.json, so that a check whose tool prints absolute paths is not
// refused for it (2026-10-17, STG walk M-9: ESLint warnings). One rule, used
// by both, so the two cannot drift.
package localpath

import (
	"regexp"
	"strings"
)

// pattern: a drive letter counts only when no letter precedes it, so
// "http://" and "foo:/" do not.
var pattern = regexp.MustCompile(`/Users/|/home/|/private/|/var/folders/|/tmp/|~/|(?:^|[^A-Za-z])[A-Za-z]:[/\\]`)

// Find reports the first local path in body: the match and up to 40 bytes
// after it.
func Find(body []byte) (string, bool) {
	loc := pattern.FindIndex(body)
	if loc == nil {
		return "", false
	}
	end := loc[1] + 40
	if end > len(body) {
		end = len(body)
	}
	return string(body[loc[0]:end]), true
}

// Mask replaces every local path prefix in s with a marker that keeps it
// readable and that Find no longer matches: "/home/" becomes "<home>/",
// "/var/folders/" "<var-folders>/", "~/" "<~>/", "C:\" "<C:>\". The rest of
// each path is kept. A marker's slash can join what follows into another
// prefix ("/private/var/folders/" becomes "<private>/var/folders/"), so it
// masks again until nothing is left. Each pass that changes s removes a
// slash or a drive letter's colon-slash, so len(s)+1 passes always suffice.
func Mask(s string) string {
	for range len(s) + 1 {
		next := maskOnce(s)
		if next == s {
			break
		}
		s = next
	}
	return s
}

func maskOnce(s string) string {
	return pattern.ReplaceAllStringFunc(s, func(m string) string {
		if n := len(m); n >= 3 && m[n-2] == ':' {
			// A drive letter, perhaps with the character before it.
			return m[:n-3] + "<" + m[n-3:n-1] + ">" + m[n-1:]
		}
		name := strings.ReplaceAll(strings.Trim(m, "/"), "/", "-")
		return "<" + name + ">/"
	})
}
