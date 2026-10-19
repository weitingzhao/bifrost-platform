package agentd

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// What an attempt hands in as text (TD-382). The platform refuses a report
// or a review that names a path of this machine (H8, localpath), and an
// executor's report quotes paths whenever it cites a log line, an error or
// a file it read: on STG (M-22, 2026-10-19) a passing cursor review was
// refused twice, its attempt voided and leased again. So agentd hands in a
// report as a check hands in its output (withoutLocalPaths): the attempt's
// checkout ".", its task's directory with the siblings "..", the executor's
// home "$HOME" and temporary directory "$TMPDIR" (whose "/tmp/" H8 would
// match), then whatever H8 still matches masked, colour sequences dropped
// first. The log tail, which the platform keeps as it is, is handed
// in the same way, so that no text agentd uploads names this machine's paths.
// A check's verify.json is masked where it is written (handIn), and a patch
// is kept as it is.
//
// What the platform would still refuse is withheld, as handIn withholds a
// check's outputs (review 380 A1): a line that names a task's workspace in a
// form the rewrite does not know (".../tasks//<task>/a1", "/./", a JSON
// escape) is replaced, and a report the platform would still refuse after
// that, or one over its 1 MiB once masked and cut at a line (review 380 A2),
// is handed in as a note and the report's verdict line.

// handText is body, an artifact of kind that a's executor wrote, as agentd
// uploads it.
func (a *Attempt) handText(kind string, body []byte) []byte {
	switch kind {
	case "report", "review", "log_tail":
	default:
		return body
	}
	if body == nil {
		return nil
	}
	paths := newPathRewriteAt(a.Dir, map[string]string{"$HOME": filepath.Join(a.Out, "home"), "$TMPDIR": filepath.Join(a.Out, "tmp")})
	plain := sgr.ReplaceAllString(string(body), "")
	out := []byte(localpath.Mask(paths.rewrite(plain)))
	if kind == "log_tail" {
		return out
	}
	// A reader of the report sees quoted code or logs as agentd rewrote
	// them (a pattern "/Users/|/home/" reads "<Users>/|<home>/"); one line
	// at the top says so (STG walk 3, 2026-10-19).
	if string(out) != plain {
		out = append([]byte(rewrittenNote), out...)
	}
	if refusedText(out) {
		lines := strings.Split(string(out), "\n")
		for i, l := range lines {
			if refusedText([]byte(l)) {
				lines[i] = withheldLine
			}
		}
		out = []byte(strings.Join(lines, "\n"))
	}
	verdict := verdictLine(body)
	if len(out) > maxBody {
		keep := []byte(strings.ToValidUTF8(string(out[:maxBody-len(truncatedNote)-1-len(verdict)]), ""))
		if i := bytes.LastIndexByte(keep, '\n'); i >= 0 {
			keep = keep[:i+1]
		} else {
			keep = append(keep, '\n')
		}
		out = append(append(keep, truncatedNote...), verdict...)
	}
	if refusedText(out) || len(out) > maxBody {
		out = append([]byte(withheldReport), verdict...)
	}
	return out
}

const (
	rewrittenNote  = "(agentd rewrote this machine's paths in this report: the attempt's directories as ., .., $HOME, $TMPDIR; other path prefixes as <Users>/, <home>/, <tmp>/ and the like, quoted code and logs included)\n"
	withheldLine   = "(withheld by agentd: this line still named a path of this machine or a task's workspace after masking)"
	withheldReport = "(withheld by agentd: after masking, the report still named a path of this machine or a task's workspace)\n"
	truncatedNote  = "(cut by agentd: the report was over 1 MiB once masked)\n"
)

// refusedText: the platform's scans of a report (H8, T11) refuse body.
func refusedText(body []byte) bool {
	_, local := localpath.Find(body)
	_, names := localpath.WorkspaceRef(body)
	return local || names
}

// verdictLine is report's verdict line (reviewVerdict) with its newline, or
// "" when it ends on none.
func verdictLine(report []byte) string {
	lines := strings.Split(strings.TrimSpace(string(report)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last == VerdictPass || last == VerdictFail {
		return last + "\n"
	}
	return ""
}
