package agentthreads

import (
	"testing"
	"time"
)

// TestPinLookNoticeText records one silent-thread push (verb Look).
func TestPinLookNoticeText(t *testing.T) {
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := time.Date(2026, 10, 11, 4, 15, 0, 0, time.UTC)
	title, body := DefaultConfig().Message(Thread{
		Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
		Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120,
		At: at, TurnStartedAt: at,
	}, now)
	wantTitle := "Look · claude 7e939cd5 stopped 11m ago"
	wantBody := "Nothing is working on it. Open the thread.\n" +
		"claude on mbp · W-65 · silent 11m · last event before_tool Bash (timeout 120s) at 04:04 UTC · in turn 11m. Nothing was restarted."
	if title != wantTitle || body != wantBody {
		t.Fatalf("title %q\nbody %q", title, body)
	}
}
