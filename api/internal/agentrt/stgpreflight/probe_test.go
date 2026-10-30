package stgpreflight

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

func TestReadOnlyTransport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/v1/namespaces/cicd/configmaps/bifrost-release-window", true},
		{"POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", true},
		{"POST", "/apis/authentication.k8s.io/v1/selfsubjectreviews", true},
		{"POST", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns?dryRun=All", true},
		{"PATCH", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns/stgrel-1-1-aaaaaaaa?dryRun=All", true},
		{"POST", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns", false},
		{"POST", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns?dryRun=All&dryRun=", false},
		{"PATCH", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns/stgrel-1-1-aaaaaaaa", false},
		{"PATCH", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns/stgrel-1/status?dryRun=All", false},
		{"POST", "/api/v1/namespaces/default/serviceaccounts/default/token", false},
		{"PUT", "/api/v1/namespaces/cicd/configmaps/bifrost-release-window", false},
		{"DELETE", "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns/stgrel-1", false},
		{"GET", "/api/v1/namespaces/default/pods/p/exec", false},
		{"GET", "/api/v1/namespaces/default/pods/p/proxy", false},
		{"GET", "/api/v1/namespaces/default/pods/p/attach", false},
		{"GET", "/api/v1/namespaces/default/pods/p/portforward", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()
			calls := 0
			tr := ReadOnlyTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
			}))
			r, err := http.NewRequest(tc.method, "https://cluster.invalid"+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := tr.RoundTrip(r)
			if response != nil {
				response.Body.Close()
			}
			if (err == nil) != tc.allowed || calls != map[bool]int{true: 1, false: 0}[tc.allowed] {
				t.Fatalf("allowed=%t calls=%d error=%v", tc.allowed, calls, err)
			}
		})
	}
}

func TestProbeRejectsUnrelatedIdentityBeforeAdmission(t *testing.T) {
	t.Parallel()
	for _, username := range []string{"admin", "system:serviceaccount:bifrost-platform-stg:workers", ""} {
		t.Run(username, func(t *testing.T) {
			t.Parallel()
			var calls []string
			client := &http.Client{Transport: ReadOnlyTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				body := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"platform-workers","uid":"deployment-uid"},"spec":{"template":{"spec":{"serviceAccountName":"workers"}}}}`
				if len(calls) == 2 {
					body = `{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview","status":{"userInfo":{"username":"` + username + `"}}}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}))}
			results := Run(context.Background(), &rest.Config{Host: "https://cluster.invalid"}, client, Options{WorkersNamespace: "bifrost-platform-prod"})
			if len(results) != 1 || results[0].Passed || results[0].Name != "workers_identity" || !strings.Contains(results[0].Reason, "system:serviceaccount:bifrost-platform-prod:workers") || len(calls) != 2 || calls[1] != "POST /apis/authentication.k8s.io/v1/selfsubjectreviews" {
				t.Fatalf("identity refusal failed or performed downstream I/O: results=%+v calls=%v", results, calls)
			}
		})
	}
}
