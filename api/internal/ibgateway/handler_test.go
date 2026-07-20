package ibgateway

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestAssessSocketFeedQualityStaleHeartbeat(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	snap := `{"host_connected":true,"accounts_snapshot":[{"account_id":"U1"}],"updated_at":1699999990}`
	q := assessSocketFeedQuality(
		"live",
		map[string]string{"connected": "True", "client_id": "70", "last_msg_ts": "1699999800"},
		map[string]string{"host_connected": "True", "host_client_id": "70", "last_msg_ts": "1699999800"},
		`{"bid":100,"ask":101,"last":100.5,"ts":1699999900}`,
		snap,
		now,
	)
	if q.Reach != probe.ReachFail {
		t.Fatalf("expected fail on stale heartbeat, got %s (%s)", q.Reach, q.Reason)
	}
}

func TestAssessSocketFeedQualityMissingClientID(t *testing.T) {
	now := time.Now().UTC()
	snap := fmt.Sprintf(`{"host_connected":true,"accounts_snapshot":[{"account_id":"U1"}],"updated_at":%d}`, now.Unix())
	q := assessSocketFeedQuality(
		"live",
		map[string]string{"connected": "True", "last_msg_ts": fmt.Sprintf("%d", now.Unix())},
		map[string]string{"host_connected": "True", "last_msg_ts": fmt.Sprintf("%d", now.Unix())},
		`{"bid":100,"ask":101,"last":100.5,"ts":`+fmt.Sprintf("%d", now.Unix())+`}`,
		snap,
		now,
	)
	if q.Reach != probe.ReachFail {
		t.Fatalf("expected fail without client_id, got %s", q.Reach)
	}
}

func TestAssessSocketFeedQualityEmptyAccountSnapshot(t *testing.T) {
	now := time.Now().UTC()
	snap := fmt.Sprintf(
		`{"host_connected":true,"secondary_connected":true,"accounts_snapshot":[],"updated_at":%d}`,
		now.Unix(),
	)
	q := assessSocketFeedQuality(
		"live",
		map[string]string{"connected": "True", "client_id": "70", "last_msg_ts": fmt.Sprintf("%d", now.Unix())},
		map[string]string{"host_connected": "True", "host_client_id": "70", "last_msg_ts": fmt.Sprintf("%d", now.Unix())},
		fmt.Sprintf(`{"bid":-1,"ask":-1,"last":201.26,"ts":%d}`, now.Unix()),
		snap,
		now,
	)
	if q.Reach != probe.ReachFail {
		t.Fatalf("expected fail on empty accounts_snapshot, got %s (%s)", q.Reach, q.Reason)
	}
	if !strings.Contains(q.Reason, "ghost") && !strings.Contains(q.Reason, "empty") {
		t.Fatalf("expected ghost/empty reason, got %s", q.Reason)
	}
}

func TestAssessSocketFeedQualityMockSkips(t *testing.T) {
	q := assessSocketFeedQuality("mock", map[string]string{"connected": "False"}, nil, "", "", time.Now().UTC())
	if q.Reach != probe.ReachOK {
		t.Fatalf("mock mode should skip feed quality, got %s", q.Reach)
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
