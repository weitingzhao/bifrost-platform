package actions

import (
	"strings"
	"testing"
)

func TestRollingRebootCatalogEntry(t *testing.T) {
	a, ok := ByID("rolling_reboot")
	if !ok {
		t.Fatal("rolling_reboot missing from the catalog")
	}
	if a.Tier != TierD {
		t.Fatalf("tier = %s, want D", a.Tier)
	}
	if a.Method != "" || a.Pattern != "" {
		t.Fatalf("rolling_reboot must not be a direct HTTP route, got %s %s", a.Method, a.Pattern)
	}
	got := RollingRebootResult("appr-1")
	command, _ := got["command"].(string)
	if !strings.Contains(command, "--execute --approval appr-1") {
		t.Fatalf("command = %q", command)
	}
	if got["executed_by_platform"] != false {
		t.Fatalf("platform must not execute the reboot: %#v", got)
	}
	msg, _ := got["message"].(string)
	if !strings.Contains(msg, command) {
		t.Fatalf("message = %q", msg)
	}
}
