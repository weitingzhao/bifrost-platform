package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// wireExternalRecords supplies bounded, read-only lists for durable backfill.
func (s *Server) wireExternalRecords(src *controller.Sources) {
	src.Questions = s.questionRuntime.agentRuntimeQuestionObservations()
	if s.approvals != nil {
		reads := newBoundedReads(2)
		src.Approvals = func(ctx context.Context) ([]store.ApprovalFact, error) {
			items, ok, err := within(ctx, reads, func() ([]approvals.Approval, bool, error) {
				items, err := s.approvals.Observations()
				return items, true, err
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("approval observations unavailable")
			}
			now := time.Now()
			var out []store.ApprovalFact
			for _, a := range items {
				if f, ok := approvalObservation(a, now); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
	if s.delivery != nil {
		src.PipelineRuns = func(ctx context.Context) ([]store.PipelineRun, error) {
			items, err := s.delivery.PipelineRunObservations(ctx)
			if err != nil {
				return nil, err
			}
			var out []store.PipelineRun
			for _, item := range items {
				if f, ok := pipelineRunFact(item); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
}

func approvalObservation(a approvals.Approval, now time.Time) (store.ApprovalFact, bool) {
	// Leave time transitions to the sweeping writer, whose DecidedAt is
	// the sweep time. A stale file must never reverse its terminal record.
	if a.Status == approvals.StatusPending && now.After(a.ExpiresAt) {
		return store.ApprovalFact{}, false
	}
	if a.Status == approvals.StatusApproved && a.Execution != nil && !a.Execution.Deadline.IsZero() && now.After(a.Execution.Deadline) {
		return store.ApprovalFact{}, false
	}
	return approvalFact(a)
}

var pipelineReason = regexp.MustCompile("^[A-Za-z]{1,64}$")

func pipelineRunFact(run unstructured.Unstructured) (store.PipelineRun, bool) {
	f := store.PipelineRun{Run: run.GetName(), UID: string(run.GetUID()), Kind: "other", CreatedAt: run.GetCreationTimestamp().Time}
	f.Pipeline, _, _ = unstructured.NestedString(run.Object, "spec", "pipelineRef", "name")
	switch {
	case strings.HasPrefix(f.Pipeline, "bifrost-ci-"):
		f.Kind = "ci"
	case strings.HasPrefix(f.Pipeline, "bifrost-deliver-"):
		f.Kind = "delivery"
	}
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, value := range conditions {
		c, ok := value.(map[string]any)
		if !ok || c["type"] != "Succeeded" {
			continue
		}
		switch c["status"] {
		case "True":
			f.Result = "Succeeded"
		case "False":
			f.Result = "Failed"
		default:
			return store.PipelineRun{}, false
		}
		reason, _ := c["reason"].(string)
		if pipelineReason.MatchString(reason) {
			f.Reason = reason
		}
		break
	}
	if f.Result == "" {
		return store.PipelineRun{}, false
	}
	readTime := func(key string) time.Time {
		value, _, _ := unstructured.NestedString(run.Object, "status", key)
		at, _ := time.Parse(time.RFC3339, value)
		return at
	}
	f.StartedAt, f.CompletedAt = readTime("startTime"), readTime("completionTime")
	if f.Result == "Failed" && f.StartedAt.IsZero() {
		f.StartedAt = f.CreatedAt
	}
	f.Repo = run.GetLabels()["bifrost.io/repo"]
	if f.Repo == "" {
		f.Repo = map[string]string{
			"bifrost-deliver-platform":      "bifrost-platform",
			"bifrost-deliver-platform-prod": "bifrost-platform",
		}[f.Pipeline]
	}
	params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, value := range params {
		p, ok := value.(map[string]any)
		if !ok {
			continue
		}
		v, _ := p["value"].(string)
		switch p["name"] {
		case "revision":
			f.Revision = v
		}
	}
	results, _, _ := unstructured.NestedSlice(run.Object, "status", "results")
	for _, value := range results {
		r, _ := value.(map[string]any)
		v, _ := r["value"].(string)
		switch r["name"] {
		case "platform-tree":
			f.Tree = v
		case "tested-by":
			f.TestedBy = v
		}
	}
	return f, true
}
