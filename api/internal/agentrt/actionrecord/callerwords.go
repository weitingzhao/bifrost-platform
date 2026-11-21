package actionrecord

import (
	"encoding/json"
	"strings"
)

// traceWords are the body fields a caller writes for the record, not for the
// action: why, what undoes it, what was not checked.
var traceWords = []string{"reason", "rollback", "unchecked"}

// CallerWords reads traceWords from a request body. The catalog's Normalize
// keeps only an action's declared params, so these were dropped before the
// record was written: every record of 11-15 to 11-19 said "(the caller gave
// no reason)" and regret_state undeclared, though callers sent all three
// (2026-11-19, LANE-ci-w1; 823 rows read).
func CallerWords(raw []byte) map[string]any {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	out := map[string]any{}
	for _, k := range traceWords {
		if v, ok := body[k].(string); ok && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

// WithCallerWords is params with the caller's words added where params has
// none; the action's own params win.
func WithCallerWords(params, said map[string]any) map[string]any {
	if len(said) == 0 {
		return params
	}
	out := make(map[string]any, len(params)+len(said))
	for k, v := range said {
		out[k] = v
	}
	for k, v := range params {
		out[k] = v
	}
	return out
}
