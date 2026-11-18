// Package deliverymetrics holds the delivery measurement records: the
// metric readings, the metrics_computed event that accounts for them, and the
// sealed baseline of 2026-11-09. It moved out of store on 2026-11-18; the
// delivery domain owns these event types (OWNER_LAW #28).
package deliverymetrics

import (
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
)

const computedType = "metrics_computed"

var (
	metricID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	sha256ID = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		// metric_label_set has no writer and no body: its name stays for the
		// database's closed list of types.
		Types: []string{computedType, "metric_label_set"},
		Bodies: map[string][]eventlog.Field{
			computedType: {
				{Name: "source", Kind: eventlog.Str},
				{Name: "rows", Kind: eventlog.Int},
				{Name: "definitions", Kind: eventlog.Any},
				{Name: "windows", Kind: eventlog.Any, Optional: true},
				{Name: "label", Kind: eventlog.Str, Optional: true},
				{Name: "artifact_id", Kind: eventlog.ID, Optional: true},
				{Name: "xmin", Kind: eventlog.Int, Optional: true},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			computedType: {
				"source":      eventlog.OneOf("computed", "imported"),
				"label":       eventlog.Shaped("a metric label", regexp.MustCompile(`^(baseline|group):[A-Za-z0-9._-]{1,64}$`)),
				"artifact_id": eventlog.Shaped("an artifact id (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)),
			},
		},
		Before: map[string]func(map[string]any) error{computedType: checkDefinitions},
	})
}

// checkDefinitions holds definitions to an array of well-formed definitions.
func checkDefinitions(body map[string]any) error {
	var defs []MetricDefinition
	if err := strictjson.Value(body["definitions"], &defs); err != nil {
		return eventlog.Invalid("metrics_computed definitions: %v", err)
	}
	if defs == nil {
		return eventlog.Invalid("metrics_computed definitions must be an array")
	}
	for _, d := range defs {
		if !metricID.MatchString(d.ID) || d.Version < 1 || !sha256ID.MatchString(d.SHA256) {
			return eventlog.Invalid("invalid metric definition")
		}
	}
	return nil
}
