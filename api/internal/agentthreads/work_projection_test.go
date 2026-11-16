package agentthreads

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"reflect"
	"testing"
)

func TestWorksFromIdentitiesPreservesOrderAndIdentity(t *testing.T) {
	t.Parallel()
	ids := []workregistration.Identity{{ID: "w2", Root: "r", Node: "n", State: "active", Host: "h", Vendor: "v", Session: "s", Epoch: 42}, {ID: "w1", Epoch: -1}}
	want := []Work{{ID: "w2", Root: "r", Node: "n", State: "active", Host: "h", Vendor: "v", Session: "s", Epoch: 42}, {ID: "w1", Epoch: -1}}
	got := WorksFromIdentities(ids)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("identity changed: %+v", got)
	}
	got[0].ID = "other"
	if ids[0].ID != "w2" {
		t.Fatal("output aliases source")
	}
	if empty := WorksFromIdentities(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty list became null: %v", empty)
	}
}
