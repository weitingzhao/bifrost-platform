package delivery

import (
	"context"
	"strings"
	"time"
)

// StartDeliveryIfFree starts a delivery run for the platform's own loop
// (internal/headrelease), which holds no release window: the run takes the
// window itself, as any start without a holder does (run_window.go). When a
// live window is held, by a session or by another run, nothing is started and
// heldBy names the holder: the loop waits, it does not take a window from
// anyone (2026-11-13: a commit waited thirteen minutes on windows two lines
// held; the loop must not become a third contender).
//
// principal is who the started-run hook is told started it. The start is the
// service's own StartPipelineRun: every check a start over HTTP gets, it gets.
func (h *Handler) StartDeliveryIfFree(ctx context.Context, principal, pipeline, revision string, params map[string]string) (run PipelineRunView, heldBy string, err error) {
	found, window, err := h.svc.readReleaseWindow(ctx)
	if err != nil {
		return PipelineRunView{}, "", err
	}
	if holder, _ := window["who"].(string); found && strings.TrimSpace(holder) != "" && !releaseWindowExpired(window, time.Now().UTC()) {
		return PipelineRunView{}, strings.TrimSpace(holder), nil
	}
	_, run, err = h.svc.StartPipelineRun(ctx, pipeline, revision, "", "", params)
	if err != nil {
		return PipelineRunView{}, "", err
	}
	if h.onPipelineStarted != nil {
		h.onPipelineStarted(pipeline, revision, run.Name, principal, "")
	}
	return run, "", nil
}
