// Package landingci reads Tekton CI runs for the two things that still use
// it: the daily full CI (full.go, failure.go) and the release policy's check
// of a run that reused another run's tests (reuse.go). The landing lane's own
// CI client (pinned runs, S5, grants) was retired with that lane
// (LANE-BIZ-ALIGN, 2026-11-13). It does not import delivery: the server
// supplies only Kubernetes transports.
package landingci

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Client's factories are lazy; construction reads neither credentials nor K8s.
type Client struct {
	Namespace string
	Dynamic   func() (dynamic.Interface, error)
	// Logs reads the end of a container's log in a pod of Namespace, for
	// a red run's failure (Failure); nil, the failure is told by its
	// messages alone.
	Logs func(ctx context.Context, pod, container string) (string, error)
	// FullScopeParam has a full run ask for api-scope-request=full
	// (EnsureFullRun, AGENTRT_FULL_CI_PARAM).
	FullScopeParam bool
}

func (c *Client) namespace() string {
	if c.Namespace != "" {
		return c.Namespace
	}
	return "cicd"
}
func (c *Client) resource() (dynamic.ResourceInterface, error) {
	if c.Dynamic == nil {
		return nil, fmt.Errorf("landing CI transport unavailable")
	}
	d, err := c.Dynamic()
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("landing CI transport unavailable")
	}
	return d.Resource(pipelineRunGVR).Namespace(c.namespace()), nil
}

type Evidence struct {
	landing.CIRun
	InfraCommit string
	Results     map[string]string
	Reason      string
}

// strictCandidate is the repository-aware Read filter, not the legacy
// candidate predicate. Legacy only uses it to downgrade conflicting evidence.
func strictCandidate(item unstructured.Unstructured, repo, revision string) bool {
	if _, inline, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "pipelineSpec"); inline {
		return false
	}
	params, counts := namedValues(item, "spec", "params")
	label := item.GetLabels()["bifrost.io/repo"]
	return counts["revision"] == 1 && params["revision"] == revision && (label == "" || label == repo)
}

// pipelineRuns is shared by pinned landing reads, legacy reads and release
// selection. Never truncate the list: a newer red run may be on the last page.
func (c *Client) pipelineRuns(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error) {
	res, err := c.resource()
	if err != nil {
		return nil, err
	}
	var items []unstructured.Unstructured
	seenPages := map[string]bool{}
	continuation := ""
	for {
		list, err := res.List(ctx, metav1.ListOptions{Limit: 500, Continue: continuation})
		if err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			name, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
			if name != pipeline {
				continue
			}
			items = append(items, item)
		}
		continuation = list.GetContinue()
		if continuation == "" {
			break
		}
		if seenPages[continuation] {
			return nil, fmt.Errorf("landing CI pagination repeated")
		}
		seenPages[continuation] = true
	}
	return items, nil
}
func runStatus(item unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	status, count := "Unknown", 0
	for _, cond := range conds {
		m, _ := cond.(map[string]any)
		if m["type"] != "Succeeded" {
			continue
		}
		count++
		switch m["status"] {
		case "True":
			status = "Succeeded"
		case "False":
			status = "Failed"
		case "Unknown":
			if m["reason"] == "Running" || m["reason"] == "Started" || m["reason"] == "Pending" {
				status = "Running"
			}
		}
	}
	if count != 1 {
		return "Unknown"
	}
	return status
}

// ClaimsReuse: the run reports itself a reuse, a tested-by result naming a
// run. The reuse step writes an empty tested-by when it reused nothing (TD-533:
// a Pipeline result naming a result its task did not write fails the run), so
// one empty string is an ordinary run. A repeated tested-by, or one whose value
// is not a string, still claims a reuse, which the reuse checks refuse.
func ClaimsReuse(item unstructured.Unstructured) bool {
	list, _, _ := unstructured.NestedSlice(item.Object, "status", "results")
	n, named := 0, false
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		if name, _ := m["name"].(string); name != "tested-by" {
			continue
		}
		n++
		if s, isString := m["value"].(string); !isString || strings.TrimSpace(s) != "" {
			named = true
		}
	}
	return n > 1 || named
}

// namedValues keeps counts so duplicates can never masquerade as one result.
// A result value is read without surrounding whitespace: a step that writes
// "incremental\n" to its result file means incremental (2026-11-06, B5 was
// blocked on such a run). Parameters are identity and are read as they are.
func namedValues(item unstructured.Unstructured, fields ...string) (map[string]string, map[string]int) {
	list, _, _ := unstructured.NestedSlice(item.Object, fields...)
	values, counts := map[string]string{}, map[string]int{}
	results := len(fields) > 0 && fields[len(fields)-1] == "results"
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		if results {
			value = strings.TrimSpace(value)
		}
		counts[name]++
		values[name] = value
	}
	return values, counts
}
