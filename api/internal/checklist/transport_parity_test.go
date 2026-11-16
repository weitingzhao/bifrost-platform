package checklist

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type observedBody struct {
	io.Reader
	closed *int
	reads  *int
}

func (b observedBody) Read(p []byte) (int, error) { *b.reads++; return b.Reader.Read(p) }
func (b observedBody) Close() error               { *b.closed++; return nil }

type observationTransport func(*http.Request) (*http.Response, error)

func (f observationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestChecklistTransportMatchesOriginal(t *testing.T) {
	for _, status := range []int{200, 201, 204, 301, 400, 503} {
		for _, body := range []string{`{"value":1}`, `{"value":1} trailing`, "", `{bad`} {
			for _, health := range []bool{false, true} {
				run := func(original bool) (any, string, int, int) {
					closed, reads := 0, 0
					p := &Prober{Base: "http://example.test", Client: &http.Client{Transport: observationTransport(func(r *http.Request) (*http.Response, error) {
						if r.Method != "GET" || r.URL.Path != "/observed" {
							t.Fatal("request changed")
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: observedBody{strings.NewReader(body), &closed, &reads}, Request: r}, nil
					})}}
					if health {
						var signal, detail string
						if original {
							signal, detail = p.originalHTTPOK(context.Background(), p.Base+"/observed")
						} else {
							signal, detail = p.httpOK(context.Background(), p.Base+"/observed")
						}
						return signal, detail, closed, reads
					}
					var out any
					var err error
					if original {
						err = p.originalGetJSON(context.Background(), "/observed", &out)
					} else {
						err = p.getJSON(context.Background(), "/observed", &out)
					}
					e := ""
					if err != nil {
						e = err.Error()
					}
					return out, e, closed, reads
				}
				a, b, c, d := run(true)
				x, y, z, w := run(false)
				if !reflect.DeepEqual(a, x) || b != y || c != z || d != w {
					t.Fatalf("status %d health %v: %#v %q %d %d != %#v %q %d %d", status, health, a, b, c, d, x, y, z, w)
				}
			}
		}
	}
}
func TestChecklistTransportTimeoutAndInvalidRequestParity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	for _, target := range []string{srv.URL, "http://bad\nurl"} {
		p := &Prober{Base: target, Client: &http.Client{Timeout: 20 * time.Millisecond}}
		a, b := p.originalHTTPOK(context.Background(), target)
		x, y := p.httpOK(context.Background(), target)
		if a != x || b != y {
			t.Fatalf("status error %q %q != %q %q", a, b, x, y)
		}
		var old, new any
		oe := p.originalGetJSON(context.Background(), "", &old)
		ne := p.getJSON(context.Background(), "", &new)
		if oe == nil || ne == nil || oe.Error() != ne.Error() {
			t.Fatalf("JSON errors %v != %v", oe, ne)
		}
	}
}
