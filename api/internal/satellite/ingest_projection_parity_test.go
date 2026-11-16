package satellite

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestIngestProjectionMatchesOriginal(t *testing.T) {
	check := func(items []map[string]any) {
		t.Helper()
		want, got := originalIngestProjection(items), buildIngestDeepFromRaw(items)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("objects %#v != %#v", want, got)
		}
		a, e := json.Marshal(want)
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(got)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("JSON %s != %s", a, b)
		}
	}
	check(nil)
	check([]map[string]any{})
	check([]map[string]any{nil, {"id": 42, "process_active": false, "runtime_status": 3, "runtime_externally_managed": "true"}})
	for _, runtime := range []string{"active", "policy-off", "managed", "degraded", "inactive", "unknown", "", " ACTIVE "} {
		for _, active := range []string{"active", "activating", "inactive", "failed", "deactivating", "unknown", "", " ACTIVE "} {
			for _, external := range []bool{false, true} {
				for _, gateway := range []bool{false, true} {
					for _, display := range []string{"", "  ", " supplied "} {
						check([]map[string]any{{"id": "service", "runtime_status": runtime, "process_active": active, "runtime_externally_managed": external, "platform_gateway_managed": gateway, "display_active": display, "runtime_kind": "fixture", "redis_control_env": "fixture"}})
					}
				}
			}
		}
	}
	// Original aggregation is order-sensitive: a prior degraded stops before fail.
	for _, a := range []string{"active", "inactive", "degraded", "unknown"} {
		for _, b := range []string{"active", "inactive", "degraded", "unknown"} {
			check([]map[string]any{{"id": "first", "runtime_status": a}, {"id": "second", "runtime_status": b}})
		}
	}
}
