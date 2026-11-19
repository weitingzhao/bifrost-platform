// Package approvalfacts is what the agent runtime records of the approvals it
// does not own: the fields of an approval it may keep, as seenfacts.
package approvalfacts

import (
	"regexp"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

var approvalRepo = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)
var approvalTask = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)
var approvalOp = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)

// Fact is what the runtime records of an approval: its identity and state, and
// the repo, task and op of its params when they have the shape of one; nothing
// else of the params is copied.
func Fact(a approvals.Approval) (seenfacts.ApprovalFact, bool) {
	if a.Number == 0 || a.Status == "" {
		return seenfacts.ApprovalFact{}, false
	}
	get := func(key string, re *regexp.Regexp) string {
		v, _ := a.Params[key].(string)
		if re.MatchString(v) {
			return v
		}
		return ""
	}
	task := get("task_id", approvalTask)
	if task == "" {
		task = get("task", approvalTask)
	}
	return seenfacts.ApprovalFact{Number: int64(a.Number), Status: a.Status, Action: a.Action, Tier: a.Tier, Env: a.Env, Repo: get("repo", approvalRepo), Task: task, Op: get("op", approvalOp), CreatedAt: a.CreatedAt, DecidedAt: a.DecidedAt, Expired: a.Status == approvals.StatusExpired}, true
}

// Observation is Fact of an approval read now: a record whose terminal state
// is only a matter of time is not one yet.
func Observation(a approvals.Approval, now time.Time) (seenfacts.ApprovalFact, bool) {
	// Leave time transitions to the sweeping writer, whose DecidedAt is
	// the sweep time. A stale file must never reverse its terminal record.
	if a.Status == approvals.StatusPending && now.After(a.ExpiresAt) {
		return seenfacts.ApprovalFact{}, false
	}
	if a.Status == approvals.StatusApproved && a.Execution != nil && !a.Execution.Deadline.IsZero() && now.After(a.Execution.Deadline) {
		return seenfacts.ApprovalFact{}, false
	}
	return Fact(a)
}
