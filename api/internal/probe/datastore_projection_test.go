package probe

import (
	"context"
	"reflect"
	"testing"
)

func TestFallbackCallSequenceIsPreserved(t *testing.T) {
	cases := []struct {
		name, reach           string
		present               bool
		lan                   string
		replies               []string
		wantCalls             []string
		wantReach, wantDetail string
	}{
		{"cluster-ok", "ok", true, "lan", nil, nil, "ok", "cluster_api: cluster"},
		{"degraded-lan-fails", "degraded", true, "lan", []string{"fail"}, []string{"lan"}, "degraded", "cluster_api: cluster · tcp_lan: tcp"},
		{"failed-lan-recovers", "fail", true, "lan", []string{"ok"}, []string{"lan"}, "ok", "cluster_api: cluster · tcp_lan verified"},
		{"second-lan-recovers", "fail", true, "lan", []string{"fail", "ok"}, []string{"lan", "lan"}, "ok", "tcp_lan: tcp"},
		{"both-lan-fail", "unknown", true, "lan", []string{"fail", "fail", "unknown"}, []string{"lan", "lan", "config.svc.cluster.local"}, "unknown", "tcp_config (in-cluster DNS — use cluster_api from Mac host): tcp"},
		{"missing-row", "", false, "", []string{"fail"}, []string{"config.svc.cluster.local"}, "fail", "tcp_config (in-cluster DNS — use cluster_api from Mac host): tcp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls []string
			got := projectDatastore(context.Background(), "env", "config.svc.cluster.local", "postgres", func() (datastoreRow, bool) {
				return datastoreRow{Reach: c.reach, Detail: "cluster", LAN: c.lan}, c.present
			}, func(_ context.Context, id, category, addr string) datastoreReading {
				if id != "postgres" || category != "datastore" {
					t.Fatal("caller metadata changed")
				}
				calls = append(calls, addr)
				if len(calls) > len(c.replies) {
					t.Fatal("extra fallback")
				}
				return datastoreReading{Reach: c.replies[len(calls)-1], Detail: "tcp", URL: "tcp://" + addr}
			})
			if !reflect.DeepEqual(calls, c.wantCalls) || got.Reach != c.wantReach || got.Detail != c.wantDetail {
				t.Fatalf("got calls=%v result=%+v", calls, got)
			}
		})
	}
}
