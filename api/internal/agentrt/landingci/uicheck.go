package landingci

import (
	"context"
	"fmt"

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

// EnsureUICheck creates the check of ui commit ui with platform commit
// platform: a full run of bifrost-ci-platform (no base, so every test runs,
// the console's among them) with uiRevision set to the commit, and returns
// its name. An existing run of that name with this spec is taken as started.
func (c *Client) EnsureUICheck(ctx context.Context, platform, ui string) (string, error) {
	if !fullSHA.MatchString(platform) || !fullSHA.MatchString(ui) {
		return "", fmt.Errorf("invalid ui check revision")
	}
	name := UICheckRunName(platform, ui)
	params := map[string]string{"revision": platform, "uiRevision": ui, "repo-url": platformRepoURL}
	labels := map[string]string{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/repo": "bifrost-platform",
		"bifrost.io/trigger": UICheckTrigger, "bifrost.io/revision": platform, "bifrost.io/ui-revision": ui}
	return name, c.create(ctx, name, "bifrost-ci-platform", params, labels)
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
