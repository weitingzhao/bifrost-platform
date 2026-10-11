package approvals

import (
	"fmt"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// CanonicalApprovalLine is the one line the server shows for a request.
// Control characters, Unicode line separators (U+2028, U+2029) and
// bidirectional controls are escaped, and the line ends with the first 12
// hex characters of the params hash. Those 12 characters are a display
// digest (48 bits), not a uniqueness proof: two param sets can share a
// prefix. The server compares the full params hash. Callers compare the
// line with strict equality.
func CanonicalApprovalLine(a Approval) string {
	ref := a.ID
	if a.Number != 0 {
		ref = fmt.Sprintf("#%d", a.Number)
	}
	parts := []string{ref, "tier " + orMark(a.Tier), a.Action}
	if a.Env != "" {
		parts = append(parts, "env "+escapeVisible(a.Env))
	}
	if a.Summary != "" {
		parts = append(parts, escapeVisible(a.Summary))
	}
	if len(a.KeyParams) > 0 {
		keys := make([]string, 0, len(a.KeyParams))
		for k := range a.KeyParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kv := make([]string, 0, len(keys))
		for _, k := range keys {
			kv = append(kv, escapeVisible(k)+"="+escapeVisible(a.KeyParams[k]))
		}
		parts = append(parts, strings.Join(kv, ", "))
	}
	line := strings.Join(parts, " · ")
	sum := a.ParamsHash
	if len(sum) > 12 {
		sum = sum[:12]
	}
	if sum == "" {
		sum = "000000000000"
	}
	return line + " · " + sum
}

func orMark(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// escapeVisible makes a field safe to show on one line. The rules live in notice.
func escapeVisible(s string) string { return notice.EscapeVisible(s) }
