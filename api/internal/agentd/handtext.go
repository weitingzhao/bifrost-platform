package agentd

import (
	"path/filepath"

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
	return []byte(localpath.Mask(paths.rewrite(sgr.ReplaceAllString(string(body), ""))))
}
