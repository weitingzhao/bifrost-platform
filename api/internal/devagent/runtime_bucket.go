package devagent

import "strings"

const (
	RuntimeBucketRunning = "running"
	RuntimeBucketReady   = "ready"
	RuntimeBucketIdle    = "idle"
	RuntimeBucketSettled = "settled"
)

func runtimeJobStatusFromJob(job *Job) string {
	if job == nil {
		return ""
	}
	switch job.Status {
	case JobRunning:
		return "running"
	case JobAwaitingReview:
		return "awaiting_review"
	case JobFailed:
		return "failed"
	default:
		return ""
	}
}

func isLiveRunningJobStatus(jobStatus string) bool {
	return jobStatus == "running" || jobStatus == "awaiting_review"
}

func pendingAndPromptReady(bp *ProgramBlueprint, done map[string]bool) (pendingCount int, promptReady bool) {
	if bp == nil {
		return 0, false
	}
	for _, p := range bp.Phases {
		if done[p.ID] {
			continue
		}
		pendingCount++
		if strings.TrimSpace(p.PromptTemplate) != "" {
			promptReady = true
		}
	}
	return pendingCount, promptReady
}

// ClassifyRuntimeBucket maps a program's live job + phase readiness into a delivery trace bucket.
// Failed live jobs are not running; remaining rules apply (typically ready or idle).
func ClassifyRuntimeBucket(jobStatus, programStatus string, allPhasesDone, promptReady bool, pendingCount int) string {
	if isLiveRunningJobStatus(jobStatus) {
		return RuntimeBucketRunning
	}
	if strings.EqualFold(strings.TrimSpace(programStatus), "completed") || allPhasesDone {
		return RuntimeBucketSettled
	}
	if promptReady && pendingCount > 0 {
		return RuntimeBucketReady
	}
	return RuntimeBucketIdle
}
