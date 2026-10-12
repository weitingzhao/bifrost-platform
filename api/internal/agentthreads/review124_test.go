package agentthreads

import (
	"testing"
	"time"
)

// Planted violation: assign t.At = at in Apply's pending-tool progress guard.
func TestReview124GuardBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	base := Beat{Thread: "run-review124", Vendor: "codex", Host: "mbp", Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 1800, TurnID: "T", Seq: 1}
	key := Key(base.Vendor, base.Thread)
	t.Run("anchor-and-order", func(t *testing.T) {
		st := State{}
		st.Apply(base, t0)
		progress := base
		progress.Event = AfterTool
		progress.Tool = "headless-progress"
		progress.ToolTimeoutS = 0
		progress.Seq = 2
		if !st.Apply(progress, t0.Add(time.Minute)) {
			t.Fatal("progress rejected")
		}
		if st.Apply(progress, t0.Add(2*time.Minute)) {
			t.Fatal("duplicate accepted")
		}
		if !st.Threads[key].At.Equal(t0) {
			t.Error("progress moved timeout anchor")
		}
		if cfg.StatusOf(st.Threads[key], false, t0.Add(32*time.Minute+time.Second)) != Silent {
			t.Error("timeout extended")
		}
		end := progress
		end.Event = TurnEnd
		end.Seq = 3
		if !st.Apply(end, t0.Add(2*time.Minute)) || st.Threads[key].Event != TurnEnd {
			t.Fatal("real later event rejected")
		}
	})
	for _, tc := range []struct {
		name       string
		at         time.Duration
		event      Event
		tool, turn string
		accepted   bool
	}{
		{"exact-expiry", 30 * time.Minute, AfterTool, "headless-progress", "T", true},
		{"expired", 31 * time.Minute, AfterTool, "headless-progress", "T", true},
		{"other-turn", time.Minute, AfterTool, "headless-progress", "U", false},
		{"real-after", time.Minute, AfterTool, "Bash", "T", true},
		{"non-progress-event", time.Minute, TurnEnd, "headless-progress", "T", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := State{}
			st.Apply(base, t0)
			b := base
			b.Event = tc.event
			b.Tool = tc.tool
			b.ToolTimeoutS = 0
			b.TurnID = tc.turn
			b.Seq = 2
			if st.Apply(b, t0.Add(tc.at)) != tc.accepted {
				t.Fatal("wrong acceptance")
			}
			if tc.accepted && (st.Threads[key].Event != tc.event || st.Threads[key].ToolTimeoutS != 0) {
				t.Fatal("guard swallowed real or expired beat")
			}
		})
	}
}
