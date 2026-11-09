package agentrtapi_test

import (
	"net/http"
	"testing"
)

func TestDeliveryBoundariesRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	if code, _ := call(t, s, "", "GET", "delivery?view=boundaries", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", code)
	}
	if code, out := call(t, s, "viewer", "GET", "delivery?view=boundaries", nil); code != http.StatusOK || out["as_of"] == nil || out["tuning"] == nil {
		t.Fatalf("snapshot: %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "GET", "delivery?view=boundaries&task=x", nil); code != http.StatusBadRequest {
		t.Fatalf("mixed query: %d", code)
	}
	if code, _ := call(t, s, "viewer", "GET", "delivery", nil); code != http.StatusBadRequest {
		t.Fatalf("legacy query: %d", code)
	}
	for _, selector := range []string{"task", "commit"} {
		if code, _ := call(t, s, "viewer", "GET", "delivery?view=configuration&"+selector+"=x", nil); code != http.StatusBadRequest {
			t.Fatalf("configuration mixed query %s: %d", selector, code)
		}
	}
}
