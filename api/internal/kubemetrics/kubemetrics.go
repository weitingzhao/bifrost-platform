// Package kubemetrics counts the platform's own requests to the Kubernetes
// API by verb, resource and calling site (2026-11-16: the cluster's
// pipelineruns LIST rose to about 9300 per 10 minutes and the apiserver's
// metrics could not say who sent them; each switch of a reader to the bus is
// to be measured before and after). A site is named with WithSite on the
// request's context; one not named counts as "-".
package kubemetrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/client-go/rest"
)

type siteKey struct{}

// WithSite names the code that makes the requests on ctx.
func WithSite(ctx context.Context, site string) context.Context {
	return context.WithValue(ctx, siteKey{}, site)
}

func siteOf(ctx context.Context) string {
	if s, ok := ctx.Value(siteKey{}).(string); ok && s != "" {
		return s
	}
	return "-"
}

type key struct{ verb, resource, site string }

var counts sync.Map // key -> *atomic.Int64

func count(k key) { v, _ := counts.LoadOrStore(k, new(atomic.Int64)); v.(*atomic.Int64).Add(1) }

// Instrument wraps cfg's transport so every request is counted. Safe to call
// more than once on different configs.
func Instrument(cfg *rest.Config) *rest.Config {
	if cfg == nil {
		return cfg
	}
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper { return counter{next: rt} })
	return cfg
}

type counter struct{ next http.RoundTripper }

func (c counter) RoundTrip(r *http.Request) (*http.Response, error) {
	verb, resource := Classify(r.Method, r.URL.Path, r.URL.Query().Get("watch") == "true")
	count(key{verb, resource, siteOf(r.Context())})
	return c.next.RoundTrip(r)
}

// Classify reads the verb and resource of a Kubernetes API request:
// GET on a collection is LIST (WATCH with watch=true), on a named object GET.
func Classify(method, path string, watch bool) (verb, resource string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /api/v1/[namespaces/<ns>/]<res>[/<name>[/<sub>]]
	// /apis/<group>/<version>/[namespaces/<ns>/]<res>[/<name>[/<sub>]]
	i := 0
	switch {
	case len(parts) >= 2 && parts[0] == "api":
		i = 2
	case len(parts) >= 3 && parts[0] == "apis":
		i = 3
	default:
		return strings.ToUpper(method), "other"
	}
	if len(parts) > i+1 && parts[i] == "namespaces" {
		if len(parts) == i+2 {
			resource = "namespaces"
			return named(method, watch), resource
		}
		i += 2
	}
	if i >= len(parts) {
		return strings.ToUpper(method), "discovery"
	}
	resource = parts[i]
	if len(parts) == i+1 {
		if method == http.MethodGet {
			if watch {
				return "WATCH", resource
			}
			return "LIST", resource
		}
		return strings.ToUpper(method), resource
	}
	return named(method, watch), resource
}

func named(method string, watch bool) string {
	if method == http.MethodGet {
		if watch {
			return "WATCH"
		}
		return "GET"
	}
	return strings.ToUpper(method)
}

// Write adds bifrost_kube_requests_total to a text exposition.
func Write(b *strings.Builder) {
	var lines []string
	counts.Range(func(k, v any) bool {
		kk := k.(key)
		lines = append(lines, fmt.Sprintf("bifrost_kube_requests_total{verb=%q,resource=%q,site=%q} %d", kk.verb, kk.resource, kk.site, v.(*atomic.Int64).Load()))
		return true
	})
	sort.Strings(lines)
	b.WriteString("# HELP bifrost_kube_requests_total Requests this process sent to the Kubernetes API, by verb, resource and calling site\n")
	b.WriteString("# TYPE bifrost_kube_requests_total counter\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
}
