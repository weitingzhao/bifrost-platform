package externalwork

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
)

// notedType is the event type of an external work's record on its stream
// (external-work:<id>): the note of its registration, each heartbeat and each
// event, as ExternalNote. One owning domain per event type (OWNER_LAW #28;
// migration 0032, 2026-11-17): it was a note with an external_work field,
// whose shape the plan's note check held through a port; the lineage
// declares and checks its own record now. The old rows stay and every reader
// reads both.
const notedType = "external_work_noted"

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{notedType},
		Bodies: map[string][]eventlog.Field{
			notedType: {{Name: "text", Kind: eventlog.Str}, {Name: "external_work", Kind: eventlog.Any}},
		},
		Before: map[string]func(map[string]any) error{notedType: checkNotedBody},
	})
}

// checkNotedBody holds the record to ExternalNote's shape and rules.
func checkNotedBody(body map[string]any) error {
	var n ExternalNote
	if err := strictjson.Value(body["external_work"], &n); err != nil {
		return err
	}
	return n.Validate()
}
