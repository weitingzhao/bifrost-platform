package agentrtapi

import "context"

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"

func (a *API) recordHandinRefusal(ctx context.Context, task, attempt string, err error) {
	if recordErr := store.RecordHandinRefusal(ctx, a.DB, task, attempt, err); recordErr != nil {
		a.logf("agentrt: hand-in refusal could not be recorded: %v", recordErr)
	}
}
