package actor

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

// registeredType records actors joining the runtime: the seed, a thread that
// registers itself, a configured seat.
const registeredType = "actor_registered"

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{registeredType},
		Bodies: map[string][]eventlog.Field{
			registeredType: {{Name: "actors", Kind: eventlog.IDs}, {Name: "capability_facts", Kind: eventlog.IDs, Optional: true},
				{Name: "note", Kind: eventlog.Str, Optional: true}, {Name: "token", Kind: eventlog.Str, Optional: true}},
		},
	})
}
