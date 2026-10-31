package delivery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// STGSampleWindowMinutes is the sample's window lease. The sample actions
// run once per approval, so the lease covers a whole deliver run instead of
// being renewed every minute; release_window_hold with the same who extends
// it when a run takes longer.
const STGSampleWindowMinutes = windowTTLMax

// HeldRuns names every PipelineRun in cicd that still carries the B5 hold
// finalizer, across all pages. A label is not trusted: a held run without
// B5 labels still blocks a new sample.
func (a *STGReleaseAdapter) HeldRuns(ctx context.Context) ([]string, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	options := metav1.ListOptions{Limit: 100}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			if slices.Contains(page.Items[i].GetFinalizers(), STGReleaseFinalizer) {
				out = append(out, page.Items[i].GetName())
			}
		}
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			slices.Sort(out)
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("STG run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// ReadRun is a plain GET of one cicd PipelineRun that keeps NotFound apart
// from every other failure.
func (a *STGReleaseAdapter) ReadRun(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return nil, err
	}
	return dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).Get(ctx, name, metav1.GetOptions{})
}

// HoldSampleWindow takes or renews the release window for one sample who
// through the ordinary window write: a live window of another who refuses
// (*WindowHeld). It never writes a B5 generation, so B5's own lease
// generations are unaffected by a sample.
func (a *STGReleaseAdapter) HoldSampleWindow(ctx context.Context, who, reason string) (time.Time, error) {
	if !strings.HasPrefix(who, "stgrel-bifrost-platform-") || strings.HasPrefix(reason, stgWindowGenerationPrefix) {
		return time.Time{}, fmt.Errorf("invalid STG sample window")
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	cs, err := a.core()
	if err != nil {
		return time.Time{}, err
	}
	rec, err := putReleaseWindow(ctx, cs, STGReleaseNamespace, windowInput{What: "bifrost-platform", Who: who, Env: "stg", Reason: reason, TTLMinutes: STGSampleWindowMinutes}, a.now())
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, rec.ExpiresAt)
}

// EndSampleWindow ends the window only while that same sample who holds it
// live. Its own read is a precheck: deleteReleaseWindow reads the ConfigMap
// again, refuses if another who holds it live by then (WindowNotHolder), and
// ends it with an Update on that second read's resourceVersion, so a change
// after the second read is a conflict (WindowChanged), never overwritten.
// Another holder's window is left alone and reported as not ended. A
// non-empty reason must also equal the record's reason on the precheck
// read; "" accepts any (a renewal by release_window_hold may change it).
func (a *STGReleaseAdapter) EndSampleWindow(ctx context.Context, who, reason string) (bool, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	cs, err := a.core()
	if err != nil {
		return false, err
	}
	now := a.now()
	rec, live, err := getReleaseWindow(ctx, cs, STGReleaseNamespace, now)
	if err != nil || !live || rec.Who != who || reason != "" && rec.Reason != reason {
		return false, err
	}
	if err = deleteReleaseWindow(ctx, cs, STGReleaseNamespace, who, false, now); err != nil {
		return false, err
	}
	return true, nil
}
