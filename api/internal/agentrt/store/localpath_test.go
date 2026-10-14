package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// H8: the closed rule finds every local path form in the artifacts that are
// scanned, and nothing in the ones that are not or in look-alikes.
func TestLocalPath(t *testing.T) {
	for _, hit := range []string{
		"see /Users/someone/x", "in /home/ci/build", "/private/tmp/a", "at /var/folders/xy/T/z", "wrote /tmp/out.txt",
		"open ~/agent-work", "C:\\Users\\x", "D:/data", "path=E:/x",
	} {
		if _, found := store.LocalPath("report", []byte(hit)); !found {
			t.Errorf("missed %q", hit)
		}
	}
	for _, miss := range []string{
		"https://example.com/a", "see foo:/bar", "ratio 3:/4", "the home directory", "users/alice", "tmp files", "a~b",
	} {
		if p, found := store.LocalPath("report", []byte(miss)); found {
			t.Errorf("%q matched %q", miss, p)
		}
	}
	for _, kind := range []string{"log_tail", "checkpoint_patch", "task_spec", "brief"} {
		if _, found := store.LocalPath(kind, []byte("/Users/someone")); found {
			t.Errorf("kind %s was scanned", kind)
		}
	}
}
