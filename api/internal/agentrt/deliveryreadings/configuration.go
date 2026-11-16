package deliveryreadings

// ConfigurationRowInput contains only fields already selected by the caller's registered selectors.
type ConfigurationRowInput struct {
	ID, Revision, Path, Selector, Unit, Object, Source, Installed, SourceError, InstalledError string
	SourceKnown, InstalledKnown                                                                bool
}
type ConfigurationBoundaryObservation struct {
	ID    string
	Equal func(string, string) bool
}

func ConfigurationRow(in ConfigurationRowInput, equal func(string, string) bool) map[string]any {
	row := map[string]any{"boundary_id": in.ID, "repo": "bifrost-trade-infra", "revision": in.Revision, "path": in.Path, "selector": in.Selector, "unit": in.Unit, "object": in.Object, "source_value": nil, "installed_value": nil, "state": "unknown", "adjustment": "versioned_configuration"}
	if in.SourceKnown {
		row["source_value"] = in.Source
	} else {
		row["source_error"] = "Registered selector unavailable in Git"
	}
	if in.InstalledKnown {
		row["installed_value"] = in.Installed
	} else {
		row["installed_error"] = "Installed value is unknown"
	}
	if in.SourceError != "" {
		row["source_error"] = in.SourceError
	}
	if in.InstalledError != "" {
		row["installed_error"] = in.InstalledError
	}
	if in.SourceKnown && in.InstalledKnown {
		if equal(in.Source, in.Installed) {
			row["state"] = "source_matches_installed"
		} else {
			row["state"] = "source_differs_from_installed"
		}
	}
	return row
}
func ObserveConfigurationRequests(requests []map[string]any, rows []map[string]any, boundaries []ConfigurationBoundaryObservation) {
	for _, request := range requests {
		request["delivery_state"] = "unknown"
		wanted, ok := request["request"].(map[string]string)
		if !ok {
			continue
		}
		for i, boundary := range boundaries {
			if boundary.ID != wanted["boundary_id"] {
				continue
			}
			installed, installedOK := rows[i]["installed_value"].(string)
			source, sourceOK := rows[i]["source_value"].(string)
			switch {
			case installedOK && boundary.Equal(installed, wanted["value"]):
				request["delivery_state"] = "requested_value_observed"
			case sourceOK && installedOK && boundary.Equal(source, wanted["value"]):
				request["delivery_state"] = "awaiting_deployment"
			case sourceOK && installedOK:
				request["delivery_state"] = "awaiting_source_change"
			}
		}
	}
}
