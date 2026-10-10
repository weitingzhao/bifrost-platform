package actions

import (
	"fmt"
	"strings"
)

// ObjectName is the DNS-1123 name of at most 63 characters an approval
// execution uses for a PipelineRun or Job. The suffix is the approval id and
// the attempt, so a retry of the same attempt reads the same object.
func ObjectName(base string, att CreateAttempt) string {
	id := strings.TrimPrefix(att.ApprovalID, "appr_")
	id = dnsLabel(id)
	if len(id) > 16 {
		id = id[:16]
	}
	if id == "" {
		id = "approval"
	}
	attempt := att.Attempt
	if attempt < 1 {
		attempt = 1
	}
	suffix := fmt.Sprintf("-%s-%d", id, attempt)
	b := dnsName(base)
	if len(b)+len(suffix) > 63 {
		b = strings.Trim(b[:63-len(suffix)], "-.")
	}
	if b == "" {
		b = "run"
	}
	return b + suffix
}

func dnsName(name string) string {
	name = dnsLabel(name)
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if name == "" {
		return "job"
	}
	return name
}

func dnsLabel(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-._")
	if s == "" {
		return "unknown"
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-._")
	}
	return s
}
