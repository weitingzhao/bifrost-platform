package launchd

import "testing"

func TestParseKeepsOnlyBifrostLabels(t *testing.T) {
	list := "" +
		"PID\tStatus\tLabel\n" +
		"4242\t0\tcom.bifrost.operator-plane\n" +
		"-\t0\tcom.bifrost.peer-watchdog\n" +
		"7\t0\tcom.apple.something\n" +
		"not-a-row\n"
	plists := "/Users/vision/Library/LaunchAgents/com.bifrost.remediation-runner.plist\n" + // 已退役 fixture
		"/Users/vision/Library/LaunchAgents/com.bifrost.operator-plane.plist\n"
	got := Parse(list, plists)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Label != "com.bifrost.operator-plane" || !got[0].Running || got[0].PID != 4242 || !got[0].Plist {
		t.Fatalf("operator-plane: %+v", got[0])
	}
	if got[1].Label != "com.bifrost.peer-watchdog" || got[1].Running || got[1].PID != 0 || got[1].Plist {
		t.Fatalf("peer-watchdog: %+v", got[1])
	}
	if got[2].Label != "com.bifrost.remediation-runner" || got[2].Running || !got[2].Plist || got[2].LastExit != -1 { // 已退役 fixture
		t.Fatalf("plist-only: %+v", got[2])
	}
}
