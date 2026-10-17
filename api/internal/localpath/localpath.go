// Package localpath is the closed search rule H8 (DESIGN-host-minimal §H8):
// a path that points into one machine. The platform refuses artifacts that
// contain one (agentrt/store); agentd masks them in the output it hands in as
// verify.json, so that a check whose tool prints absolute paths is not
// refused for it (2026-10-17, STG walk M-9: ESLint warnings). One rule, used
// by both, so the two cannot drift.
package localpath

import (
	"bytes"
	"encoding/json"
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
// masks again until nothing is left. Each pass that changes s lowers the
// count of slashes, "~/" pairs and a drive letter's colon-slash pairs, which
// starts at most at len(s), so len(s)+1 passes always suffice (review 237 C).
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

// jsonDrive is H8's drive-letter form completed by a JSON escape: the encoder
// writes "C:<" as "C:\u003c", a control character or an invalid byte after
// the colon likewise starts with a backslash.
var jsonDrive = regexp.MustCompile(`(?:^|[^A-Za-z])[A-Za-z]:\\`)

// MaskJSON masks what H8 matches in body, a JSON document whose strings Mask
// has already cleaned: what is left there is a drive letter's colon followed
// by an escape the encoder wrote. It puts a space after that colon, which
// leaves every escape whole, so the document stays valid. ok is false when
// the result is not valid JSON or H8 still matches it.
func MaskJSON(body []byte) (out []byte, ok bool) {
	out = body
	for range len(body) + 1 {
		next := jsonDrive.ReplaceAllFunc(out, func(m []byte) []byte {
			n := len(m)
			r := make([]byte, 0, n+1)
			r = append(r, m[:n-1]...)
			r = append(r, ' ')
			return append(r, m[n-1])
		})
		if bytes.Equal(next, out) {
			break
		}
		out = next
	}
	_, found := Find(out)
	return out, !found && json.Valid(out)
}
