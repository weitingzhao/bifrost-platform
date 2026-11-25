package landingci

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UICheckTrigger labels the runs that check a new bifrost-ui commit with
// platform's main before the stable pin moves to it (agentrt/uicheck, R0E).
const UICheckTrigger = "agentrt-ui-check"

// ErrRunGone is RunStatus's answer for a run the cluster no longer holds.
var ErrRunGone = fmt.Errorf("the PipelineRun is gone")

// UICheckRunName names the check of ui at platform: one run per pair.
func UICheckRunName(platform, ui string) string { return "ui-check-" + ui[:12] + "-" + platform[:8] }

// UIPushTrigger is the Gitea trigger that runs bifrost-ci-platform at
// platform's main with the commit a bifrost-ui push moved to (infra
// trigger-trade-ci.yaml, platform-ci-ui).
const UIPushTrigger = "platform-ci-ui"

// EnsureUICheck creates the check of ui commit ui with platform commit
// platform: a full run of bifrost-ci-platform (no base, so every test runs,
// the console's among them) with uiRevision set to the commit, and returns
// its name. An existing run of that name with this spec is taken as started.
//
// A ui push already runs that check (UIPushTrigger, at platform's main then),
// so the newest such run of this ui commit that has not failed is the check
// and nothing is created: every ui push ran the whole platform suite twice
// (ci-platform-fzzks and ui-check-03dc06fd2372-f493175e, 2026-11-25). A
// failed one is checked again with a run of its own, so a flake of the push
// run does not keep the commit off the pin.
func (c *Client) EnsureUICheck(ctx context.Context, platform, ui string) (string, error) {
	if !fullSHA.MatchString(platform) || !fullSHA.MatchString(ui) {
		return "", fmt.Errorf("invalid ui check revision")
	}
	name := UICheckRunName(platform, ui)
	if res, err := c.resource(); err != nil {
		return "", err
	} else if _, err := res.Get(ctx, name, metav1.GetOptions{}); err != nil && apierrors.IsNotFound(err) {
		if pushed, err := c.uiPushRun(ctx, ui); err != nil {
			return "", err
		} else if pushed != "" {
			return pushed, nil
		}
	}
	params := map[string]string{"revision": platform, "uiRevision": ui, "repo-url": platformRepoURL}
	labels := map[string]string{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/repo": "bifrost-platform",
		"bifrost.io/trigger": UICheckTrigger, "bifrost.io/revision": platform, "bifrost.io/ui-revision": ui}
	return name, c.create(ctx, name, "bifrost-ci-platform", params, labels)
}

// uiPushRun is the newest run of UIPushTrigger with uiRevision ui that has
// not failed, "" when there is none.
func (c *Client) uiPushRun(ctx context.Context, ui string) (string, error) {
	items, err := c.pipelineRuns(ctx, "bifrost-ci-platform", "triggers.tekton.dev/trigger="+UIPushTrigger)
	if err != nil {
		return "", err
	}
	best, bestAt := "", time.Time{}
	for _, item := range items {
		params, counts := namedValues(item, "spec", "params")
		if counts["uiRevision"] != 1 || params["uiRevision"] != ui || runStatus(item) == "Failed" {
			continue
		}
		if at := item.GetCreationTimestamp().Time; best == "" || at.After(bestAt) {
			best, bestAt = item.GetName(), at
		}
	}
	return best, nil
}

// RunStatus is the status of PipelineRun name: Succeeded, Failed, Running or
// Unknown; ErrRunGone when the cluster no longer holds it.
func (c *Client) RunStatus(ctx context.Context, name string) (string, error) {
	res, err := c.resource()
	if err != nil {
		return "", err
	}
	obj, err := res.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", ErrRunGone
	}
	if err != nil {
		return "", err
	}
	return runStatus(*obj), nil
}
