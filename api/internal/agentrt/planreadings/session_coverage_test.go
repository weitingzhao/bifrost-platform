package planreadings

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestSessionCoverageProjection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		out    map[string]any
		reader bool
		err    error
		want   map[string]any
		calls  int
		logs   []string
	}{
		{"missing coverage", map[string]any{}, true, nil, map[string]any{}, 0, nil},
		{"no reader", map[string]any{"coverage": map[string]any{}}, false, nil, map[string]any{"coverage": map[string]any{}}, 0, nil},
		{"success", map[string]any{"coverage": map[string]any{"existing": 1}}, true, nil, map[string]any{"coverage": map[string]any{"existing": 1, "sessions": map[string]int{"linked": 2}}}, 1, nil},
		{"failed", map[string]any{"coverage": map[string]any{}, "warnings": []string{"existing"}}, true, errors.New("offline"), map[string]any{"coverage": map[string]any{}, "warnings": []string{"existing", "Session coverage is Unknown: the agent thread state could not be read"}}, 1, []string{"plan view: session coverage: offline"}},
		{"failed no warnings", map[string]any{"coverage": map[string]any{}}, true, errors.New("offline"), map[string]any{"coverage": map[string]any{}}, 1, []string{"plan view: session coverage: offline"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var logs []string
			var read func(context.Context) (any, error)
			if tc.reader {
				read = func(context.Context) (any, error) { calls++; return map[string]int{"linked": 2}, tc.err }
			}
			AppendSessionCoverage(context.Background(), tc.out, read, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
			if !reflect.DeepEqual(tc.out, tc.want) || calls != tc.calls || !reflect.DeepEqual(logs, tc.logs) {
				t.Fatalf("out=%#v logs=%v calls=%d", tc.out, logs, calls)
			}
		})
	}
}
