package research

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

const proxyTimeout = 60 * time.Second

// Proxy forwards platform-api requests to Research API (:8795).
// Prefer RESEARCH_API_URL; otherwise use K8s API service proxy to research-api.
func (s *Service) Proxy(r *http.Request, upstreamPath string) (*http.Response, error) {
	upstreamPath = "/" + strings.TrimPrefix(upstreamPath, "/")

	if base := strings.TrimRight(strings.TrimSpace(s.cfg.APIBaseURL), "/"); base != "" {
		target := base + upstreamPath
		if q := r.URL.RawQuery; q != "" {
			target = target + "?" + q
		}
		return s.proxyHTTP(r, target, nil)
	}
	return s.proxyViaKube(r, upstreamPath)
}

func (s *Service) proxyHTTP(r *http.Request, target string, transport http.RoundTripper) (*http.Response, error) {
	client := &http.Client{Timeout: proxyTimeout}
	if transport != nil {
		client.Transport = transport
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		return nil, err
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	// Do not forward platform operator Authorization to Research API.
	req.Header.Del("Authorization")
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}

func (s *Service) proxyViaKube(r *http.Request, upstreamPath string) (*http.Response, error) {
	if s.cluster == nil {
		return nil, fmt.Errorf("cluster service unavailable; set RESEARCH_API_URL for local override")
	}
	restCfg, _, err := s.cluster.RestConfig()
	if err != nil {
		return nil, err
	}

	rawPath := strings.TrimPrefix(upstreamPath, "/")
	if q := r.URL.RawQuery; q != "" {
		rawPath = rawPath + "?" + q
	}

	// /api/v1/namespaces/{ns}/services/{name}:{port}/proxy/{path}
	svcProxy := fmt.Sprintf("%s:%s", apiServiceName, apiServicePort)
	host := strings.TrimRight(restCfg.Host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	target, err := url.JoinPath(host, "api", "v1", "namespaces", pluginNamespace, "services", svcProxy, "proxy")
	if err != nil {
		return nil, err
	}
	target = strings.TrimRight(target, "/") + "/" + rawPath

	rt, err := rest.TransportFor(restCfg)
	if err != nil {
		return nil, err
	}
	return s.proxyHTTP(r, target, rt)
}
