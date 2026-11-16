package planreadings

import "context"

// AppendSessionCoverage adds a read-only projection; failed reads remain Unknown.
func AppendSessionCoverage(ctx context.Context, out map[string]any, read func(context.Context) (any, error), logf func(string, ...any)) {
	coverage, ok := out["coverage"].(map[string]any)
	if read == nil || !ok {
		return
	}
	sessions, err := read(ctx)
	if err != nil {
		logf("plan view: session coverage: %v", err)
		if warnings, ok := out["warnings"].([]string); ok {
			out["warnings"] = append(warnings, "Session coverage is Unknown: the agent thread state could not be read")
		}
		return
	}
	coverage["sessions"] = sessions
}
