package cirunview

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// UnitTaskRuns lists the TaskRuns of unit tasks in namespace: the ones
// carrying the label, whatever its value (CI design v4, landing stage 2
// block 2-C). selector narrows it further ("" for all of them). The caller
// owns the cluster client and the deadline.
func UnitTaskRuns(ctx context.Context, dyn dynamic.Interface, namespace, label, selector string) ([]unstructured.Unstructured, error) {
	if selector == "" {
		selector = label
	}
	// Paged, and each run without the copy of its Task that Tekton keeps in
	// status.taskSpec and without managedFields: the loop that reads these
	// every 30 s reads labels, conditions, times and results (the copies
	// took the workers past their memory limit, measured 2026-11-15).
	var out []unstructured.Unstructured
	options := metav1.ListOptions{LabelSelector: selector, Limit: 100}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(taskRunGVR).Namespace(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			unstructured.RemoveNestedField(page.Items[i].Object, "status", "taskSpec")
			unstructured.RemoveNestedField(page.Items[i].Object, "metadata", "managedFields")
		}
		out = append(out, page.Items...)
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("unit task run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}
