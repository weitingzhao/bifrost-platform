package planstore

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
)

// Storage retains the snapshot and read limit; the plan domain projects the next Owner time.
func ownerNext(missions []*progressMission, etas map[string]map[string]any, truncated bool, now time.Time) map[string]any {
	inputs := make([]*planpace.NextMission, 0, len(missions))
	for _, m := range missions {
		tasks := make([]*planpace.NextTask, 0, len(m.tasks))
		for _, t := range m.tasks {
			tasks = append(tasks, &planpace.NextTask{TaskID: t.TaskID, DependsOn: t.DependsOn})
		}
		inputs = append(inputs, &planpace.NextMission{ID: m.id, State: m.state, Next: m.next, Tasks: tasks})
	}
	return planpace.OwnerNext(inputs, etas, truncated, now, progressMissions)
}
