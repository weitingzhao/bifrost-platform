package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Frozen from 8b1975a5. Only local listener addresses are normalized.
func TestObservationExtractionParity(t *testing.T) {
	results := map[string]any{}
	for _, code := range []int{200, 201, 302, 401, 404, 503} {
		var request string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = fmt.Sprintf("%s %s host=%s bearer=%t", r.Method, r.URL.Path, r.Host, r.Header.Get("Authorization") != "")
			w.WriteHeader(code)
			_, _ = w.Write([]byte("body drained"))
		}))
		p := NewProber()
		env := config.Environment{NginxBase: "http://192.0.2.1", IngressHost: "fixture.invalid"}
		got := p.probeHTTP(context.Background(), "fixture", "observation", srv.URL+"/status", "fixture", env)
		got.URL = strings.ReplaceAll(got.URL, srv.URL, "<http>")
		results[fmt.Sprint("http-", code)] = struct {
			Target  Target
			Request string
		}{got, request}
		srv.Close()
	}
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("TCP observation must not issue HTTP") }))
	defer listener.Close()
	addr := listener.Listener.Addr().String()
	for _, kind := range []string{"postgres", "redis"} {
		for _, state := range []Reachability{"", ReachOK, ReachDegraded, ReachFail, ReachUnknown} {
			for _, lan := range []bool{false, true} {
				for _, canceled := range []bool{false, true} {
					ctx, cancel := context.WithCancel(context.Background())
					if canceled {
						cancel()
					}
					row := DatastoreEnvReach{Postgres: state, Redis: state, PostgresDetail: "cluster fixture", RedisDetail: "cluster fixture"}
					if lan {
						row.PostgresLAN = addr
						row.RedisLAN = addr
					}
					ds := &DatastoreSnapshot{ByEnv: map[string]DatastoreEnvReach{"fixture": row}}
					p := NewProber()
					var got Target
					if kind == "postgres" {
						got = p.probePostgres(ctx, "fixture", addr, ds)
					} else {
						got = p.probeRedis(ctx, "fixture", addr, ds)
					}
					cancel()
					got.URL = strings.ReplaceAll(got.URL, addr, "<tcp>")
					got.Detail = strings.ReplaceAll(got.Detail, addr, "<tcp>")
					results[fmt.Sprintf("%s-%s-lan%t-cancel%t", kind, state, lan, canceled)] = got
				}
			}
		}
	}
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "observation-parity.json")

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(want) {
		t.Fatal("observation response/request differs from 8b1975a5 baseline")
	}
}
