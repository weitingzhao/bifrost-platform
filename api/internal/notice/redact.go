package notice

import "regexp"

// The classes are written out so the questions MCP tool can compile the same
// set. Go and JavaScript do not agree on \s, \w, \b, or case-folding flags.
// Whitespace is space, tab, LF, CR, and FF. A word edge is the start of the
// text or one character outside [A-Za-z0-9_]. Letters are listed in both cases.
const (
	asciiWS        = "[ \t\n\r\f]"
	notSecretValue = "[^ \t\n\r\f\"',;]"
	notURLUser     = "[^/ \t\n\r\f:@]"
	notURLPass     = "[^@ \t\n\r\f/]"
	secretWords    = `[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss][Ss][Ww][Dd]|[Pp][Ww][Dd]|[Ss][Ee][Cc][Rr][Ee][Tt]|[Tt][Oo][Kk][Ee][Nn]|[Aa][Pp][Ii][_-]?[Kk][Ee][Yy]|[Aa][Cc][Cc][Ee][Ss][Ss][_-]?[Kk][Ee][Yy]|[Pp][Rr][Ii][Vv][Aa][Tt][Ee][_-]?[Kk][Ee][Yy]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll]`
)

var redactions = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), "[redacted private key]"},
	{regexp.MustCompile(`(^|[^A-Za-z0-9_])([Bb][Ee][Aa][Rr][Ee][Rr]|[Bb][Aa][Ss][Ii][Cc])` + asciiWS + `+[A-Za-z0-9._~+/=-]{8,}`), "$1$2 [redacted]"},
	{regexp.MustCompile(`(^|[^A-Za-z0-9_])([A-Za-z0-9_.-]*(?:` + secretWords + `)[A-Za-z0-9_.-]*)(["']?` + asciiWS + `*[:=]` + asciiWS + `*["']?)` + notSecretValue + `+`), "$1$2$3[redacted]"},
	{regexp.MustCompile(`(^|[^A-Za-z0-9_])eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "$1[redacted jwt]"},
	{regexp.MustCompile(`(^|[^A-Za-z0-9_])(?:AKIA|ASIA)[0-9A-Z]{16}($|[^A-Za-z0-9_])`), "$1[redacted aws key]$2"},
	{regexp.MustCompile(`(^|[^A-Za-z0-9_])(?:gh[pousr]_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,})`), "$1[redacted token]"},
	{regexp.MustCompile(`://(` + notURLUser + `+):(` + notURLPass + `+)@`), "://$1:[redacted]@"},
}

// Redact masks common secret shapes. Call it before clipping or storing:
// a tail or a clip that cuts the marker off would otherwise keep the secret.
// Controls, bidi marks and newlines are not rewritten.
func Redact(s string) string {
	for _, r := range redactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
