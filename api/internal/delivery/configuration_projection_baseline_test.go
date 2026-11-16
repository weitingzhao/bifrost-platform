package delivery

import (
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"reflect"
	"strings"
	"testing"
)

func baselineConfigurationRow(b configBoundary, head, path, object, source, live string, sourceErr, readErr error, sourceErrors, liveErrors map[string]string) map[string]any {
	row := map[string]any{"boundary_id": b.id, "repo": "bifrost-trade-infra", "revision": head, "path": path, "selector": strings.Join(b.path, "/"), "unit": b.unit, "object": object, "source_value": nil, "installed_value": nil, "state": "unknown", "adjustment": "versioned_configuration"}
	if sourceErr == nil {
		row["source_value"] = source
	} else {
		row["source_error"] = "Registered selector unavailable in Git"
	}
	if readErr == nil {
		row["installed_value"] = live
	} else {
		row["installed_error"] = "Installed value is unknown"
	}
	if sourceErrors[path] != "" {
		row["source_error"] = sourceErrors[path]
	}
	if liveErrors[object] != "" {
		row["installed_error"] = liveErrors[object]
	}
	if sourceErr == nil && readErr == nil {
		if b.equal(source, live) {
			row["state"] = "source_matches_installed"
		} else {
			row["state"] = "source_differs_from_installed"
		}
	}
	return row
}
func baselineObserveConfigurationRequests(requests []map[string]any, rows []map[string]any) {
	for _, request := range requests {
		request["delivery_state"] = "unknown"
		wanted, ok := request["request"].(map[string]string)
		if !ok {
			continue
		}
		for i, boundary := range configurationBoundaries {
			if boundary.id != wanted["boundary_id"] {
				continue
			}
			installed, installedOK := rows[i]["installed_value"].(string)
			source, sourceOK := rows[i]["source_value"].(string)
			switch {
			case installedOK && boundary.equal(installed, wanted["value"]):
				request["delivery_state"] = "requested_value_observed"
			case sourceOK && installedOK && boundary.equal(source, wanted["value"]):
				request["delivery_state"] = "awaiting_deployment"
			case sourceOK && installedOK:
				request["delivery_state"] = "awaiting_source_change"
			}
		}
	}
}

func TestConfigurationProjectionOriginalParity(t *testing.T) {
	err := errors.New("read unavailable")
	for _, b := range configurationBoundaries {
		for _, sourceErr := range []error{nil, err} {
			for _, readErr := range []error{nil, err} {
				for _, override := range []string{"", "read failed"} {
					source, live := "15m", "900s"
					if b.unit != "duration" {
						source, live = "1", "1000m"
					}
					path, object := "path", "object"
					got := deliveryreadings.ConfigurationRow(deliveryreadings.ConfigurationRowInput{ID: b.id, Revision: "head", Path: path, Selector: strings.Join(b.path, "/"), Unit: b.unit, Object: object, Source: source, Installed: live, SourceKnown: sourceErr == nil, InstalledKnown: readErr == nil, SourceError: override, InstalledError: override}, b.equal)
					want := baselineConfigurationRow(b, "head", path, object, source, live, sourceErr, readErr, map[string]string{path: override}, map[string]string{object: override})
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("row %#v != %#v", got, want)
					}
				}
			}
		}
	}
	observations := make([]deliveryreadings.ConfigurationBoundaryObservation, len(configurationBoundaries))
	for i, b := range configurationBoundaries {
		observations[i] = deliveryreadings.ConfigurationBoundaryObservation{ID: b.id, Equal: b.equal}
	}
	for _, values := range [][2]any{{nil, nil}, {"15m", nil}, {nil, "15m"}, {"15m", "15m"}, {"15m", "10m"}, {"10m", "5m"}, {"bad", "bad"}} {
		rows := make([]map[string]any, len(configurationBoundaries))
		for i := range rows {
			rows[i] = map[string]any{"source_value": values[0], "installed_value": values[1]}
		}
		requests := func() []map[string]any {
			return []map[string]any{{"request": map[string]string{"boundary_id": configurationBoundaries[0].id, "value": "15m"}}, {"request": map[string]any{"boundary_id": "wrong"}}, {"request": map[string]string{"boundary_id": "unknown", "value": "x"}}}
		}
		got, want := requests(), requests()
		deliveryreadings.ObserveConfigurationRequests(got, rows, observations)
		baselineObserveConfigurationRequests(want, rows)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("requests %#v != %#v", got, want)
		}
	}
}
