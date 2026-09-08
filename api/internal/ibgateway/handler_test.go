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

func TestSnapshotAgeSec(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := fmt.Sprintf(`{"updated_at":%d}`, now.Unix()-120)
	age, ok := snapshotAgeSec(raw, now)
	if !ok || age < 119 || age > 121 {
		t.Fatalf("expected age ~120, got %v ok=%v", age, ok)
	}
	if snapshotFresh(raw, now, 90) {
		t.Fatal("expected stale snapshot")
	}
	fresh := fmt.Sprintf(`{"updated_at":%d}`, now.Unix()-10)
	if !snapshotFresh(fresh, now, 90) {
		t.Fatal("expected fresh snapshot")
	}
}

// parseRedisHash is gone: the redis-cli text output it parsed is gone with the
// shell-out, and HGetAll returns a map directly. What still needs pinning is
// that the one write path stays closed while spine D10 is BLOCKED.
func TestOperatorWriteStaysBlocked(t *testing.T) {
	s := &Service{cfg: Config{RedisPlatformPass: "not-empty"}}
	if _, err := s.redisCLI("anything"); err == nil {
		t.Fatal("platform-api must not write the operator command stream while D10 is BLOCKED")
	}
}

func TestPatchGatewayYamlMode(t *testing.T) {
	in := "mode: mock\nredis:\n  host: x\n"
	out := patchGatewayYamlMode(in, "live")
	if !strings.Contains(out, "mode: live") || strings.Contains(out, "mode: mock") {
		t.Fatalf("unexpected yaml %q", out)
	}
}

// The platform must not carry Trade's account numbers, and must not relay a TWS
// login name if the gateway is still configured with one (Owner, 2026-09-06:
// account identity is the IB account number). Both went wrong at once before
// discovery replaced the hardcoded pair — the compiled-in ids no longer matched
// the keys the gateway wrote, so every slot read "no ib:health key".
func TestSlotAccountIDAcceptsOnlyIBAccountNumbers(t *testing.T) {
	for _, ok := range []string{"U17123565", "U8829175", "U11111111"} {
		if !ibAccountID.MatchString(ok) {
			t.Fatalf("%q should be accepted as an IB account number", ok)
		}
	}
	for _, bad := range []string{"", "wzhao1503", "vzhao1503", "u1712356", "U123", "U17123565x", " U17123565"} {
		if ibAccountID.MatchString(bad) {
			t.Fatalf("%q must not be relayed as an account id", bad)
		}
	}
}

func TestSlotsAreOrderedHostFirst(t *testing.T) {
	out := []SlotStatus{{Slot: "zzz"}, {Slot: "secondary"}, {Slot: "host"}}
	sortSlots(out)
	got := []string{out[0].Slot, out[1].Slot, out[2].Slot}
	want := []string{"host", "secondary", "zzz"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot order = %v, want %v", got, want)
		}
	}
}
