package landing

import (
	"testing"
	"time"
)

func TestConflictRetryRequiresControllerRework(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		by, reason string
		want       bool
	}{
		{"controller", "rework: attempt a_new accepted after the replay conflict", true},
		{"owner", "rework: attempt a_new accepted after the replay conflict", false},
		{"controller", "try_again", false},
	} {
		rs := phaseRecords("failed", true)
		rs[len(rs)-1]["why"] = "conflict"
		s := mustFold(t, rs)
		retry := v11("retry", "root", task1, "after_op", "land:1", "reason", tc.reason, "by", tc.by)
		err := s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: retry})
		if (err == nil) != tc.want {
			t.Fatalf("by=%s reason=%s: %v", tc.by, tc.reason, err)
		}
		if tc.want && s.Apply(Entry{At: epoch.Add(8 * time.Hour), Record: retry}) == nil {
			t.Fatal("duplicate retry accepted")
		}
	}
}
