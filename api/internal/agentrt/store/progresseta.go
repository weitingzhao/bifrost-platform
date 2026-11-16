package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"time"
)

const ETAHighFactor = planpace.ETAHighFactor

// Adapt stored progress to the plan domain without changing snapshot reads.
func missionETA(m *progressMission, usual map[string]usualTime, now time.Time) map[string]any {
	in := &planpace.ETAMission{State: m.state}
	for _, t := range m.tasks {
		task := &planpace.ETATask{TaskID: t.TaskID, Role: t.Role, Class: t.class, State: t.State, Status: t.Status, OnMain: t.OnMain != nil}
		if t.Attempt != nil {
			task.Attempt = &planpace.ETAAttempt{ElapsedS: t.Attempt.ElapsedS}
		}
		in.Tasks = append(in.Tasks, task)
	}
	observations := make(map[string]planpace.UsualTime, len(usual))
	for k, u := range usual {
		observations[k] = planpace.UsualTime{Duration: u.d, Samples: u.n}
	}
	return planpace.MissionETA(in, observations, now, UsualMinSamples)
}
