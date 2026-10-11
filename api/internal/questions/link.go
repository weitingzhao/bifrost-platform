package questions

import (
	"net/url"
	"strings"
)

// linkForbidden is true when the link's hash, or a full URL's path, names the
// approvals page, its old alias, or the questions page. Matching is the whole
// segment before '?', lowercased, not a substring of a longer token.
func linkForbidden(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if segmentForbidden(fragmentSegment(s)) {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return false
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return segmentForbidden(strings.ToLower(path))
}

func fragmentSegment(s string) string {
	i := strings.IndexByte(s, '#')
	if i < 0 {
		return ""
	}
	frag := s[i+1:]
	if q := strings.IndexByte(frag, '?'); q >= 0 {
		frag = frag[:q]
	}
	if un, err := url.PathUnescape(frag); err == nil {
		frag = un
	}
	return strings.ToLower(strings.Trim(frag, "/"))
}

func segmentForbidden(seg string) bool {
	switch seg {
	case "approvals", "maintenance", "questions":
		return true
	default:
		return false
	}
}
