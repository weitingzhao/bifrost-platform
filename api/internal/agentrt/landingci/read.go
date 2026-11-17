// Package landingci reads Tekton CI runs for the daily full CI (full.go,
// failure.go) and the runs view (runs.go). The check of a run that reused
// another run's tests went with the release policy's reading of CI by runs
// (2026-11-16): the evidence ledger records no reused run. The landing lane's own
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

// pipelineRuns is shared by pinned landing reads, legacy reads and release
// selection. Never truncate the list: a newer red run may be on the last page.
// selectors narrows the list by labels the creator of the run sets (2026-11-17:
// the daily full CI's look at its own runs, once a minute, read every run of
// the namespace, five pages of 2,058, to find a handful: 47 LISTs in ten
// minutes on PROD). Never tekton.dev/pipeline: Tekton's controller writes that
// label when it first reconciles a run, and a run it has not reached (the
// 23 minutes of 2026-11-17 00:01) would be missing from the list. What is
// kept is checked by the caller as before.
func (c *Client) pipelineRuns(ctx context.Context, pipeline string, selectors ...string) ([]unstructured.Unstructured, error) {
	selector := strings.Join(selectors, ",")
	res, err := c.resource()
	if err != nil {
		return nil, err
	}
	var items []unstructured.Unstructured
	seenPages := map[string]bool{}
	continuation := ""
	for {
		list, err := res.List(ctx, metav1.ListOptions{Limit: 500, Continue: continuation, LabelSelector: selector})
		if err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			name, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
			if name != pipeline {
				continue
			}
			// Kept without the copy of the Pipeline in its status and
			// without managedFields: neither is read here, and the copy is
			// most of a run (measured 2026-11-15: 77% of the listed bytes).
			unstructured.RemoveNestedField(item.Object, "status", "pipelineSpec")
			unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
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
