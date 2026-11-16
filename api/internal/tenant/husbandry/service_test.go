package datahusbandry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fixtureProxy struct {
	calls  []string
	bodies map[string]string
	status int
}

func (p *fixtureProxy) Proxy(r *http.Request, path string) (*http.Response, error) {
	p.calls = append(p.calls, path)
	status := p.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(p.bodies[path]))}, nil
}
func TestSnapshotLaneOrderAndReadPaths(t *testing.T) {
	p := &fixtureProxy{bodies: map[string]string{
		"/market/ingest/queue-dashboard": `{"husbandry":{"verdict":"healthy","detail":"ok"}}`,
		"/flex/config/summary":           `{"source":"secret","tokens":{"host_token_set":true}}`,
		"/flex/coverage/freshness":       `{"dimensions":[{"status":"fresh"}]}`,
		"/research/signal-health":        `{"data":{"overall":"healthy","freshness":[{"status":"fresh"}]}}`,
		"/research/orchestration/status": `{"data":{"verdict":"healthy"}}`,
	}}
	got := NewService(p, p, p).Snapshot(context.Background())
	if got.Overall != "healthy" || len(got.Lanes) != 3 {
		t.Fatalf("%+v", got)
	}
	if strings.Join(p.calls, ",") != "/market/ingest/queue-dashboard,/flex/config/summary,/flex/coverage/freshness,/research/signal-health,/research/orchestration/status" {
		t.Fatal(p.calls)
	}
	if got.Lanes[0].ID != "market_batch" || got.Lanes[1].ID != "flex_batch" || got.Lanes[2].ID != "research_olap" {
		t.Fatal(got.Lanes)
	}
}
func TestUnavailableRepresentationPreserved(t *testing.T) {
	s := NewService(nil, nil, nil)
	var out any
	err := s.getJSON(context.Background(), nil, "/example", &out)
	if _, ok := err.(simpleError); !ok || err.Error() != "proxy nil" {
		t.Fatalf("%T %v", err, err)
	}
	p := &fixtureProxy{status: 503, bodies: map[string]string{"/example": "unavailable"}}
	err = s.getJSON(context.Background(), p, "/example", &out)
	if _, ok := err.(simpleError); !ok || err.Error() != "unavailable" {
		t.Fatalf("%T %v", err, err)
	}
}
