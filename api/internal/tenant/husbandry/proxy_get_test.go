package datahusbandry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type proxyTrackedBody struct {
	io.Reader
	closed bool
}

func (b *proxyTrackedBody) Close() error { b.closed = true; return nil }

type proxyStatusError string

func (e proxyStatusError) Error() string { return string(e) }
func TestProxyGetContracts(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"success", `{"ok":true}`, 200, false}, {"redirect decoded", `{"ok":true}`, 302, false},
		{"bad json", `{`, 200, true}, {"status body preserved", "unavailable", 503, true},
		{"bounded read", strings.Repeat(" ", 2<<20) + `{"ok":true}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &proxyTrackedBody{Reader: strings.NewReader(tc.body)}
			ctx := context.WithValue(context.Background(), struct{}{}, "context")
			var dest map[string]bool
			err := proxyGet(ctx, func(r *http.Request, path string) (*http.Response, error) {
				if r.Method != "GET" || path != "/example" || r.URL.String() != "http://platform.local/example" || r.Context() != ctx {
					t.Fatal("request changed")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			}, "/example", &dest, func(s string) error { return proxyStatusError(s) })
			if (err != nil) != tc.wantErr || !body.closed {
				t.Fatalf("err=%v closed=%v", err, body.closed)
			}
			if tc.status >= 400 {
				if _, ok := err.(proxyStatusError); !ok || err.Error() != tc.body {
					t.Fatalf("status error changed: %T %v", err, err)
				}
			}
		})
	}
}
func TestProxyGetCancellationAndProxyError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := proxyGet(ctx, func(r *http.Request, _ string) (*http.Response, error) { return nil, r.Context().Err() }, "/example", nil, errors.New)
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	sentinel := errors.New("upstream")
	err = proxyGet(context.Background(), func(*http.Request, string) (*http.Response, error) { return nil, sentinel }, "/example", nil, errors.New)
	if err != sentinel {
		t.Fatalf("error identity changed: %v", err)
	}
}
