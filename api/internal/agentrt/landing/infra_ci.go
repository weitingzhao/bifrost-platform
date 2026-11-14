package landing

import ()

func validCIResults(v any) bool {
	values, ok := v.(map[string]any)
	if !ok || len(values) > 6 {
		return false
	}
	for name, value := range values {
		known := false
		for _, result := range LandingRepos["bifrost-trade-infra"].CIResults {
			known = known || result == name
		}
		s, text := value.(string)
		if !known || !text || len(s) > 256 {
			return false
		}
	}
	return true
}
