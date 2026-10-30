package stgpreflight

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"k8s.io/client-go/rest"
)

// Serial: replaces http.DefaultTransport to exercise Run's real health wiring
// without sockets. Other checks intentionally get Forbidden; only health and
// the successful workers identity are under test here.
func TestProbeHealth(t *testing.T) {
	for _, tc := range []struct {
		name, body, media, reason string
		status                    int
		err                       error
		bodyError, passed         bool
	}{
		{name: "healthy", status: 200, media: "application/json", body: `{"service":"bifrost-platform-api","status":"ok"}`, passed: true, reason: "STG service JSON status=ok"},
		{name: "unhealthy", status: 200, media: "application/json", body: `{"service":"bifrost-platform-api","status":"degraded"}`, reason: "STG health classified failed: reason=status http_status=200"},
		{name: "wrong_service", status: 200, media: "application/json", body: `{"service":"other","status":"ok"}`, reason: "STG health classified failed: reason=service http_status=200"},
		{name: "invalid_json", status: 200, media: "application/json", body: `{`, reason: "STG health classified failed: reason=json http_status=200"},
		{name: "html", status: 200, media: "text/html", body: `<html>OK</html>`, reason: "STG health classified failed: reason=json http_status=200"},
		{name: "http_failure", status: 503, reason: "STG health classified failed: reason=http http_status=503"},
		{name: "redirect", status: 302, reason: "STG health classified failed: reason=http http_status=302"},
		{name: "timeout", err: context.DeadlineExceeded, reason: "STG health classified unknown: reason=timeout http_status=0"},
		{name: "transport", err: errors.New("unreachable"), reason: "STG health classified unknown: reason=transport http_status=0"},
		{name: "body_read", status: 200, media: "application/json", bodyError: true, reason: "STG health classified unknown: reason=body_read http_status=200"},
	} {
		// Serial: each subtest owns the global health transport until cleanup.
		t.Run(tc.name, func(t *testing.T) {
			healthCalls, kubeCalls := 0, 0
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				healthCalls++
				if r.Method != http.MethodGet || r.URL.String() != "http://platform-api.bifrost-platform-stg.svc:8780/health" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatalf("unexpected health request: method=%s URL=%s authenticated=%t", r.Method, r.URL, r.Header.Get("Authorization") != "")
				}
				deadline, ok := r.Context().Deadline()
				if left := time.Until(deadline); !ok || left <= 0 || left > 10*time.Second {
					t.Fatalf("health request needs a deadline within 10 seconds: deadline=%v present=%t", deadline, ok)
				}
				if tc.err != nil {
					return nil, tc.err
				}
				var body io.Reader = strings.NewReader(tc.body)
				if tc.bodyError {
					body = iotest.ErrReader(errors.New("interrupted body"))
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{
					"Content-Type": []string{tc.media}, "Location": []string{"http://other.invalid/healthy"},
				}, Body: io.NopCloser(body), Request: r}, nil
			})
			cfg := &rest.Config{Host: "https://cluster.invalid", BearerToken: "workers-test-only", Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				kubeCalls++
				if r.URL.Host != "cluster.invalid" || r.Header.Get("Authorization") != "Bearer workers-test-only" {
					t.Fatalf("Kubernetes transport used outside its authenticated API: URL=%s", r.URL)
				}
				body, status := `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403}`, 403
				switch r.URL.Path {
				case "/apis/apps/v1/namespaces/bifrost-platform-prod/deployments/platform-workers":
					body, status = `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"platform-workers","uid":"workers-uid"},"spec":{"template":{"spec":{"serviceAccountName":"workers"}}}}`, 200
				case "/apis/authentication.k8s.io/v1/selfsubjectreviews":
					body, status = `{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview","status":{"userInfo":{"username":"system:serviceaccount:bifrost-platform-prod:workers"}}}`, 201
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			cfg.Wrap(ReadOnlyTransport)
			kube, err := rest.HTTPClientFor(cfg)
			if err != nil {
				t.Fatal(err)
			}
			results := Run(context.Background(), cfg, kube, Options{WorkersNamespace: "bifrost-platform-prod"})
			if len(results) != 10 || results[0].Name != "workers_identity" || !results[0].Passed || kubeCalls < 2 {
				t.Fatalf("probe did not pass identity and perform downstream checks: %+v; kube calls=%d", results, kubeCalls)
			}
			healthResults := 0
			for _, result := range results {
				if result.Name == "health" {
					healthResults++
					if result.Passed != tc.passed || result.Reason != tc.reason {
						t.Errorf("health=%+v; want passed=%t reason=%q", result, tc.passed, tc.reason)
					}
				}
			}
			if healthResults != 1 || healthCalls != 1 {
				t.Fatalf("expected one health result and one fixed-URL GET: results=%d calls=%d", healthResults, healthCalls)
			}
		})
	}
}
