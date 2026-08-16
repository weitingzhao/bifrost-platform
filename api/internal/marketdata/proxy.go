package marketdata

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

const (
	apiServiceName = "market-data-api"
	apiServicePort = "8790"
	proxyTimeout   = 60 * time.Second
)

// Proxy forwards Console requests to the Market Data Plugin API (:8790).
// Prefer MARKET_DATA_API_URL; otherwise use K8s API service proxy to market-data-api.
func (s *Service) Proxy(r *http.Request, pluginPath string) (*http.Response, error) {
	pluginPath = "/" + strings.TrimPrefix(pluginPath, "/")

	if base := strings.TrimRight(strings.TrimSpace(s.cfg.APIBaseURL), "/"); base != "" {
		target := base + pluginPath
		if q := r.URL.RawQuery; q != "" {
			target = target + "?" + q
		}
		return s.proxyHTTP(r, target, nil)
	}
	return s.proxyViaKube(r, pluginPath)
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
	s.applyUpstreamAuth(req, r)
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}

func isMutatingMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// applyUpstreamAuth maps Console operator auth → Plugin write-token.
// GET stays unauthenticated at Plugin. Mutating calls use MARKET_DATA_WRITE_TOKEN
// so the browser never holds the Plugin secret.
func (s *Service) applyUpstreamAuth(dst, src *http.Request) {
	dst.Header.Del("Authorization")
	if !isMutatingMethod(src.Method) {
		return
	}
	tok := strings.TrimSpace(s.cfg.WriteToken)
	if tok == "" {
		return
	}
	dst.Header.Set("Authorization", "Bearer "+tok)
	// Kubernetes API proxy overwrites Authorization with the kube token.
	// Plugin prefers this header so Mac platform-api enqueue still authenticates.
	dst.Header.Set("X-Market-Data-Write-Token", tok)
}

func (s *Service) proxyViaKube(r *http.Request, pluginPath string) (*http.Response, error) {
	if s.cluster == nil {
		return nil, fmt.Errorf("cluster service unavailable; set MARKET_DATA_API_URL for local override")
	}
	restCfg, _, err := s.cluster.RestConfig()
	if err != nil {
		return nil, err
	}

	rawPath := strings.TrimPrefix(pluginPath, "/")
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
	// Append plugin path (may include query) without re-encoding '/' in path segments incorrectly.
	target = strings.TrimRight(target, "/") + "/" + rawPath

	rt, err := rest.TransportFor(restCfg)
	if err != nil {
		return nil, err
	}
	return s.proxyHTTP(r, target, rt)
}
