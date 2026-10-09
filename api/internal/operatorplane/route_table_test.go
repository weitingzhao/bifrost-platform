package operatorplane

import (
	"net/http"
	"strings"
	"testing"
)

// The out-of-band plane no longer actuates. Operator-gated patrol writes are
// served by platform-api, not forwarded here.
func TestPlaneRouteTableHasNoOperatorRoutes(t *testing.T) {
	if len(planeRoutes()) == 0 {
		t.Fatal("plane route table is empty")
	}
	for _, rt := range planeRoutes() {
		if rt.operator {
			t.Errorf("%s %s is operator gated; the plane must not serve operator routes", rt.method, rt.pattern)
		}
		if strings.HasPrefix(rt.pattern, "/patrol") {
			t.Errorf("%s %s is a patrol route; platform-api serves those locally", rt.method, rt.pattern)
		}
	}
}

func TestPatrolWritesRequireOperator(t *testing.T) {
	var writes int
	for _, rt := range patrolRoutes() {
		if rt.method == http.MethodGet {
			continue
		}
		writes++
		if !rt.operator {
			t.Errorf("%s %s is not operator gated", rt.method, rt.pattern)
		}
	}
	if writes != 3 {
		t.Fatalf("patrol write routes = %d, want 3", writes)
	}
}
