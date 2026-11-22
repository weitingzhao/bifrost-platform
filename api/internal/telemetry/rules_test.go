package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 2026-11-22: the map compares the rule groups Prometheus has loaded with
// the manifests. The control: an answer that is not success is an error,
// never an empty list.
func TestQueryRuleGroupsReadsGroupsAndRuleNames(t *testing.T) {
	body := `{"status":"success","data":{"groups":[{"name":"compute","file":"/etc/x.yaml","rules":[{"name":"bifrost_node:home_info"},{"name":"ComputePainDbDiskFlush"}]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rules" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	got, err := NewClient(srv.URL).QueryRuleGroups(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "compute" || len(got[0].Rules) != 2 || got[0].Rules[0] != "bifrost_node:home_info" {
		t.Fatalf("%+v %v", got, err)
	}
	body = `{"status":"error","error":"down"}`
	if _, err := NewClient(srv.URL).QueryRuleGroups(context.Background()); err == nil {
		t.Error("a failed answer must be an error")
	}
}
