package landinggit

import (
	"strings"
	"testing"
)

// Review 694 R-07: the patch shown stops at ShownLimit bytes, the digest
// never does — a change in the last byte of a patch past the limit changes
// the digest though nothing of it is shown.
func TestPatchEvidenceLimitAndDigest(t *testing.T) {
	t.Parallel()
	at := strings.Repeat("x", ShownLimit)
	if p := patchEvidence(at); p.Truncated || p.Shown != at || p.Size != ShownLimit {
		t.Fatal("a patch at the limit is shown whole", p.Truncated, p.Size)
	}
	past := at + "a"
	p := patchEvidence(past)
	if !p.Truncated || p.Shown != "" || p.Size != ShownLimit+1 {
		t.Fatal("a patch past the limit is not shown", p.Truncated, p.Size)
	}
	if q := patchEvidence(at + "b"); q.SHA256 == p.SHA256 || q.Shown != "" {
		t.Fatal("the last byte past the limit is not in the digest")
	}
	if patchEvidence("a").SHA256 == patchEvidence("b").SHA256 {
		t.Fatal("digest ignores content")
	}
}
