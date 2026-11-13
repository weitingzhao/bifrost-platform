package delivery

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// StartDeliveryIfFree starts a delivery run for the platform's own loop
// (internal/headrelease), which holds no release window: the run takes the
// window itself, as any start without a holder does (run_window.go). When a
// live window is held, by a session or by another run, nothing is started and
// heldBy says what stands in the way: the loop waits, it does not take a window from
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
		return PipelineRunView{}, "the release window is held by " + strings.TrimSpace(holder), nil
	}
	// The window alone does not say that no run is under way: a run whose
	// window was not renewed (its follower stopped) lets the window expire
	// while it still builds (Codex review 2026-11-13, A1, reproduced: "both
	// live"). The loop starts nothing beside an unfinished run of the same
	// pipeline, whatever the window says.
	items, err := h.svc.PipelineRunObservations(ctx)
	if err != nil {
		return PipelineRunView{}, "", err
	}
	for _, item := range items {
		if ref, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name"); ref != pipeline {
			continue
		}
		if tektonRunFinished(item) {
			continue
		}
		return PipelineRunView{}, "run " + item.GetName() + " of " + pipeline + " is not finished", nil
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

// tektonRunFinished reports a run whose Succeeded condition is True or False.
func tektonRunFinished(run unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, value := range conditions {
		c, _ := value.(map[string]any)
		if c["type"] == "Succeeded" && (c["status"] == "True" || c["status"] == "False") {
			return true
		}
	}
	return false
}
