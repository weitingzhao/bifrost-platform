package operatorplane

import (
	"net/http"
	"testing"
)

// The plane is mounted both in platform-api and off-cluster, so its route table
// is the only place its auth is decided. Every non-GET entry actuates something
// (a runner, a deploy, a skill, an insight record) and must be operator gated.
func TestPlaneWritesRequireOperator(t *testing.T) {
	var writes int
	for _, rt := range routeTable() {
		if rt.method == http.MethodGet {
			continue
		}
		writes++
		if !rt.operator {
			t.Errorf("%s %s is not operator gated", rt.method, rt.pattern)
		}
	}
	if writes == 0 {
		t.Fatal("route table has no write routes — the check is not seeing it")
	}
}
