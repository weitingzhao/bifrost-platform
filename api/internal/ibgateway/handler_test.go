package ibgateway

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestClassifyReach(t *testing.T) {
	if classifyReach(true, true, true, true, "mock") != probe.ReachOK {
		t.Fatal("expected ok")
	}
	if classifyReach(false, true, true, true, "mock") != probe.ReachFail {
		t.Fatal("expected fail when deploy down")
	}
	if classifyReach(true, true, false, false, "live") != probe.ReachDegraded {
		t.Fatal("expected degraded in live without slots")
	}
}

func TestParseRedisHash(t *testing.T) {
	m := parseRedisHash("connected\nTrue\nmode\nmock\n")
	if m["connected"] != "True" || m["mode"] != "mock" {
		t.Fatalf("unexpected map %v", m)
	}
}

func TestPatchGatewayYamlMode(t *testing.T) {
	in := "mode: mock\nredis:\n  host: x\n"
	out := patchGatewayYamlMode(in, "live")
	if !strings.Contains(out, "mode: live") || strings.Contains(out, "mode: mock") {
		t.Fatalf("unexpected yaml %q", out)
	}
}
