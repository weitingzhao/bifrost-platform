package kubemetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

// 2026-11-16: the apiserver could not say who sent ~9300 pipelineruns LISTs
// in 10 minutes; the platform now counts its own by verb, resource and site.
func TestClassify(t *testing.T) {
	for _, c := range []struct {
		method, path string
		watch        bool
		verb, res    string
	}{
		{"GET", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns", false, "LIST", "pipelineruns"},
		{"GET", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns", true, "WATCH", "pipelineruns"},
		{"GET", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns/ci-platform-x", false, "GET", "pipelineruns"},
		{"POST", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns", false, "POST", "pipelineruns"},
		{"GET", "/api/v1/namespaces/cicd/configmaps", false, "LIST", "configmaps"},
		{"GET", "/api/v1/nodes", false, "LIST", "nodes"},
		{"GET", "/api/v1/namespaces/bifrost-bus", false, "GET", "namespaces"},
		{"GET", "/version", false, "GET", "other"},
	} {
		v, r := Classify(c.method, c.path, c.watch)
		if v != c.verb || r != c.res {
			t.Errorf("%s %s: %s %s, want %s %s", c.method, c.path, v, r, c.verb, c.res)
		}
	}
}

func TestInstrumentCountsBySite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	cfg := Instrument(&rest.Config{Host: srv.URL})
	rt, err := rest.TransportFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(WithSite(context.Background(), "test.site"), "GET", srv.URL+"/apis/tekton.dev/v1/namespaces/cicd/pipelineruns", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var b strings.Builder
	Write(&b)
	if !strings.Contains(b.String(), `bifrost_kube_requests_total{verb="LIST",resource="pipelineruns",site="test.site"} 1`) {
		t.Fatalf("metrics:\n%s", b.String())
	}
}
