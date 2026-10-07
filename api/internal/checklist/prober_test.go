package checklist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakePlatform serves the GET routes the prober reads, shaped like the live
// responses; bodies can be overridden per path.
func fakePlatform(t *testing.T, override map[string]string) *httptest.Server {
	t.Helper()
	bodies := map[string]string{
		"/api/v1/cluster": `{"api_reachability":"ok","detail":"1 elastic standby","nodes_ready":5,"nodes_total":5,"failing_pods":0}`,
		"/health":         `{"status":"ok"}`,
		"/console/":       `<html></html>`,
		"/api/v1/gitops/apps": `{"reachability":"ok","detail":"argocd-server ready","apps":[
			{"name":"app-a","sync_status":"Synced","health_status":"Healthy"},
			{"name":"app-b","sync_status":"Synced","health_status":"Healthy"}]}`,
		"/api/v1/agent/bridge": `{"runners":[{"url":"http://r1","role":"primary","status":"ok"},{"url":"http://r2","role":"standby","status":"ok"}],
			"git_bridge":{"status":"ok"},"satellite_probe_bridge":{"status":"not_configured"}}`,
		"/api/v1/cluster/postgres/backup-status": `{"signal":"ok","detail":"daily · last completed 1h ago"}`,
		"/api/v1/matrix": `{"matrices":[
			{"environment":"stg","targets":[{"id":"postgres","reachability":"fail"}]},
			{"environment":"prod","targets":[
				{"id":"nginx-spa","reachability":"ok","detail":"HTTP 200"},
				{"id":"api-monitor","reachability":"ok"},{"id":"api-market","reachability":"ok"},
				{"id":"postgres","reachability":"ok"},{"id":"redis","reachability":"ok"},
				{"id":"ib-operator-rpc","reachability":"unknown"}]}]}`,
		"/api/v1/delivery/pipelines":         `{"reachability":"ok","detail":"16 pipeline(s) in cicd"}`,
		"/api/v1/delivery/stg/smoke":         `{"reachability":"ok","detail":"stg 6/6 API domains reachable"}`,
		"/api/v1/plugins/market-data/status": `{"reachability":"ok","summary":"2/2 deployments ready"}`,
		"/api/v1/agent/hermes/readiness":     `{"ready":false,"blockers":["Hermes gateway not running"]}`,
	}
	for k, v := range override {
		bodies[k] = v
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if body == "500" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func testProber(srv *httptest.Server) *Prober {
	return &Prober{
		Base:       srv.URL,
		APIHealth:  srv.URL + "/health",
		ConsoleURL: srv.URL + "/console/",
		Env:        "prod",
		Client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func bySignalID(sigs []ItemSignal) map[string]ItemSignal {
	out := map[string]ItemSignal{}
	for _, s := range sigs {
		out[s.ItemID] = s
	}
	return out
}

func TestProberCoversTheCatalogExceptHusbandry(t *testing.T) {
	srv := fakePlatform(t, nil)
	defer srv.Close()
	got := bySignalID(testProber(srv).Probe(context.Background()))

	husbandry := map[string]bool{"market-batch-sla": true, "flex-tokens-secret": true, "research-batch-sla": true}
	for _, item := range CatalogItems {
		if husbandry[item.ID] {
			if _, ok := got[item.ID]; ok {
				t.Errorf("%s is overlaid live from data-husbandry; the prober must not write it", item.ID)
			}
			continue
		}
		if _, ok := got[item.ID]; !ok {
			t.Errorf("catalog item %s has no prober rule", item.ID)
		}
	}
	if len(got) != len(CatalogItems)-len(husbandry) {
		t.Errorf("prober wrote %d items, want %d", len(got), len(CatalogItems)-len(husbandry))
	}

	want := map[string]string{
		"cluster-api": SignalOK, "nodes-ready": SignalOK, "failing-pods": SignalOK,
		"platform-api": SignalOK, "platform-console": SignalOK, "argo-apps": SignalOK,
		"runners-ha": SignalOK, "git-bridge": SignalOK, "mac-probe-bridge": SignalUnknown,
		"db-backup-fresh": SignalOK, "postgres": SignalOK, "redis": SignalOK,
		"nginx-edge": SignalOK, "trade-apis": SignalOK, "deliver-pipeline": SignalOK,
		"stg-smoke": SignalOK, "massive-polygon": SignalOK, "ib-feed": SignalUnknown,
		"hermes-tooling": SignalDegraded,
	}
	for id, sig := range want {
		if got[id].Signal != sig {
			t.Errorf("%s = %s (%s), want %s", id, got[id].Signal, got[id].Detail, sig)
		}
		if got[id].Source != proberSource {
			t.Errorf("%s source = %q", id, got[id].Source)
		}
	}
}

func TestProberReadsOnlyItsOwnEnvironmentRow(t *testing.T) {
	// The stg row says postgres fails; a PROD prober must not report that.
	srv := fakePlatform(t, nil)
	defer srv.Close()
	got := bySignalID(testProber(srv).Probe(context.Background()))
	if got["postgres"].Signal != SignalOK {
		t.Fatalf("postgres = %s, read the wrong matrix row", got["postgres"].Signal)
	}
}

func TestProberRedWhenTheClusterSaysSo(t *testing.T) {
	srv := fakePlatform(t, map[string]string{
		"/api/v1/cluster": `{"api_reachability":"ok","nodes_ready":4,"nodes_total":5,"failing_pods":2,
			"failing_pod_details":[{"namespace":"ns","name":"p1","reason":"CrashLoopBackOff"}]}`,
		"/api/v1/gitops/apps":                    `{"reachability":"ok","apps":[{"name":"app-a","sync_status":"OutOfSync","health_status":"Healthy"}]}`,
		"/api/v1/agent/bridge":                   `{"runners":[{"role":"primary","status":"unavailable"},{"role":"standby","status":"ok"}],"git_bridge":{"status":"unavailable","error":"dial tcp: refused"},"satellite_probe_bridge":{"status":"ok"}}`,
		"/api/v1/cluster/postgres/backup-status": `{"signal":"fail","detail":"last completed 60h ago"}`,
	})
	defer srv.Close()
	got := bySignalID(testProber(srv).Probe(context.Background()))
	cases := map[string]string{
		"nodes-ready": SignalDegraded, "failing-pods": SignalDegraded, "argo-apps": SignalDegraded,
		"runners-ha": SignalDegraded, "git-bridge": SignalFail, "mac-probe-bridge": SignalOK,
		"db-backup-fresh": SignalFail,
	}
	for id, sig := range cases {
		if got[id].Signal != sig {
			t.Errorf("%s = %s (%s), want %s", id, got[id].Signal, got[id].Detail, sig)
		}
	}
	if !strings.Contains(got["failing-pods"].Detail, "ns/p1 CrashLoopBackOff") {
		t.Errorf("failing-pods detail lost the pod: %q", got["failing-pods"].Detail)
	}
}

func TestProberUnknownWhenARouteCannotBeRead(t *testing.T) {
	srv := fakePlatform(t, map[string]string{"/api/v1/cluster": "500", "/api/v1/matrix": "500"})
	defer srv.Close()
	got := bySignalID(testProber(srv).Probe(context.Background()))
	for _, id := range []string{"cluster-api", "nodes-ready", "failing-pods", "postgres", "redis", "nginx-edge", "trade-apis"} {
		if got[id].Signal != SignalUnknown {
			t.Errorf("%s = %s, an unreadable route must read unknown", id, got[id].Signal)
		}
	}
}

func TestProberMergesFreshSignalsWithoutDispatch(t *testing.T) {
	srv := fakePlatform(t, nil)
	defer srv.Close()
	h := NewHandler(t.TempDir(), nil)
	h.probeAndMerge(context.Background(), testProber(srv))

	resp, err := h.store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Source != proberSource {
		t.Errorf("source = %q", resp.Source)
	}
	if len(resp.LastDispatch) != 0 {
		t.Errorf("the prober dispatched %d action(s); it must only record", len(resp.LastDispatch))
	}
	for _, s := range resp.Signals {
		if s.Stale || s.ObservedAt == "" {
			t.Errorf("%s: stale=%v observed_at=%q", s.ItemID, s.Stale, s.ObservedAt)
		}
	}
}
