package evalreadings

import (
	"context"
	"fmt"
	"math"
)

// catalogDomainReadings: catalog_domain_reads, how often the map's domain page
// (GET /api/v1/catalog/domain) was read on PROD in the window. Origin: Audit,
// 2026-11-22: it checks the map line by how much the domain page is used and
// had only ad-hoc reads of this number.
func catalogDomainReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "catalog_domain_reads"
	d := def("Prometheus http_requests_total of platform-api (api/internal/httpmetrics), handler /api/v1/catalog/domain, namespace bifrost-platform-prod",
		"increase over the window, every method and status, rounded")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	sel := `http_requests_total{namespace="bifrost-platform-prod",handler="/api/v1/catalog/domain"}`
	vals, err := prom(ctx, fmt.Sprintf("sum(increase(%s[%ds] @ %d))", sel, span, w.To.Unix()))
	if err != nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	if len(vals) == 0 {
		// No series at all: the route was never hit since the process started,
		// or the series aged out. Not a measured zero.
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "no series of the route in the window"}, false)}
	}
	v := math.Round(vals[0].Value)
	return []Reading{finish(Reading{Name: name, Value: &v, Unit: "count", Samples: 1, Window: w, Definition: d}, false)}
}
