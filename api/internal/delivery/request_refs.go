package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckRequestRefs is a start_pipeline_run request's check when the request
// is made, before an approval row is written (TD-420: approval #80 signed a
// mistyped full SHA, the Owner approved it, and the run refused it half an
// hour later). params are the request's normalized params (name, revision,
// tag, who, params), read as the run reads its body. The run's own input
// rules apply as they do at the run, and every full SHA the request signs,
// its revision and each *Revision param, goes through the run's per-repository
// ref check (one mirror sync and a bounded wait for a missing one). A branch
// or tag name is not checked here: it may move or appear before the run, which
// checks it again. A Gitea or cluster that cannot be asked does not refuse.
func (s *Service) CheckRequestRefs(ctx context.Context, params map[string]any) error {
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	raw, err := json.Marshal(map[string]any{"revision": params["revision"], "tag": params["tag"], "who": params["who"],
		"params": params["params"]})
	if err != nil {
		return err
	}
	var req StartPipelineRunRequest
	if err = json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("the run would not read these params: %v", err)
	}
	revision, ciRepo := req.Revision, ""
	if isCIPipeline(name) {
		if revision, ciRepo, err = ciRunInputs(name, revision, req.Params); err != nil {
			return err
		}
	}
	if isPlatformDelivery(name) {
		if err = platformRunInputs(revision, req.Params); err != nil {
			return err
		}
	}
	rev := strings.TrimSpace(revision)
	if rev == "" {
		rev = "main"
	}
	if err = validateRevision(rev); err != nil {
		return err
	}
	if requiresFullSHA(name) && !isFullGitSHA(rev) {
		return fmt.Errorf("revision %q must be a 40-character lowercase git SHA", rev)
	}
	signed := map[string]bool{}
	if callerParamSHA.MatchString(rev) {
		signed[rev] = true
	}
	for key, value := range req.Params {
		if callerMayOverride(strings.TrimSpace(key)) && callerParamSHA.MatchString(strings.TrimSpace(value)) {
			signed[strings.TrimSpace(value)] = true
		}
	}
	if len(signed) == 0 {
		return nil
	}
	if !isPlatformDelivery(name) && !isCIPipeline(name) {
		// The run's legacy check: one revision in every repository, no sync,
		// and no *Revision param of its own.
		if !signed[rev] {
			return nil
		}
		if msg := s.legacyRunRefPreflight(ctx, name, rev); msg != "" {
			return errors.New(msg)
		}
		return nil
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil // unknown: the run checks again
	}
	pipe, err := dyn.Resource(pipelineGVR).Namespace(s.PipelinesNamespace()).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil // unknown here; the run refuses a pipeline that is not there
	}
	spec, rev, err := buildPipelineRunSpec(name, rev, req.Tag, req.Params, pipe)
	if err != nil {
		return err
	}
	repos, refs := runRefsOf(name, rev, ciRepo, spec, pipe)
	if msg := platformRefIsUIRef(name, refs); msg != "" {
		return errors.New(msg)
	}
	var check []string
	for _, repo := range repos {
		if signed[refs[repo]] {
			check = append(check, repo)
		}
	}
	sort.Strings(check)
	if len(check) == 0 {
		return nil
	}
	if msg := s.refsPresent(ctx, name, rev, ciRepo, check, refs); msg != "" {
		return errors.New(msg)
	}
	return nil
}

// CheckRequestRefs is the service's, for the approvals' creation check.
func (h *Handler) CheckRequestRefs(ctx context.Context, params map[string]any) error {
	return h.svc.CheckRequestRefs(ctx, params)
}
