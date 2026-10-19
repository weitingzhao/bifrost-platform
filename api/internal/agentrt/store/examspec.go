package store

import "time"

// The specs the chief of staff reads, written from records given rather than
// read: the chief exam (package exam) renders its fixed records with the very
// functions the runtime uses, so an exam spec and a working one differ only
// in their records (the exam's cut §4.2).

// ThreadLine is one message of the conversation a brief reads.
type ThreadLine = threadLine

// BriefSpecFor is the spec of a brief that answers body (from, kind), with the
// conversation before it and the runtime's snapshot.
func BriefSpecFor(from, kind, body string, thread []ThreadLine, snapshot string) ([]byte, error) {
	return briefSpec(from, kind, body, thread, snapshot)
}

// ScheduledBriefSpecFor is the spec of a scheduled brief whose data is data,
// as controllerBrief builds it.
func ScheduledBriefSpecFor(data map[string]any) ([]byte, error) {
	return controllerSpec(scheduledText, data)
}

// ScheduledNote is the note the controller leaves for the slot due at now.
func ScheduledNote(now time.Time) (string, bool) { return dueSlot(now) }

// PlanSpecFor is the spec of the planning task of goal g in mission.
func PlanSpecFor(g Goal, repos []string, bases map[string]string, mission, snapshot string) ([]byte, error) {
	return planSpec(g, repos, bases, mission, snapshot)
}
