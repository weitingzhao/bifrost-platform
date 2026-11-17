package eventlog_test

import (
	"encoding/json"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// An integer field takes integers that fit in an int64 and nothing that only
// looks like one: a float, a whole float, a number past int64, a digit string
// (review 145 B6: the event log's side of canon's int64 subset).
//
// Origin: moved from store's TestEventIntegersOnly on 2026-11-17, when the
// store stopped calling OfKind; the store keeps the half that goes through a
// body check.
func TestOfKindIntegersOnly(t *testing.T) {
	t.Parallel()
	for _, v := range []any{1, int64(1) << 62, json.Number("42")} {
		if !eventlog.OfKind(v, eventlog.Int) {
			t.Errorf("%#v was refused", v)
		}
	}
	for _, v := range []any{1.5, 2.0, json.Number("1.5"), json.Number("1e3"), json.Number("9223372036854775808"), "7", nil, true} {
		if eventlog.OfKind(v, eventlog.Int) {
			t.Errorf("%#v was taken as an integer", v)
		}
	}
}
