package answerack

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// SetTaskWaitForTest sets how AckUsedAnswers reads an answered card's task
// and returns the function that restores it.
func SetTaskWaitForTest(f func(ctx context.Context, db eventlog.DB, task string) (string, bool, bool, error)) func() {
	old := taskWait
	taskWait = f
	return func() { taskWait = old }
}
