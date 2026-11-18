package gatecluster

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// The unit TaskRuns, kept whole for the unit loops (reader unit-taskruns,
// 2026-11-18: each pass listed them, about 116 LISTs an hour for 10 objects).
// They have a watch of their own, under their label: they belong to no
// PipelineRun, what is read of them is more than the display cache keeps, and
// the workers need no watch of the namespace's 10,000 TaskRuns for them.

// slimUnitTask drops what no reader of a listed unit TaskRun reads, as the list does.
func slimUnitTask(obj *unstructured.Unstructured) {
	unstructured.RemoveNestedField(obj.Object, "status", "taskSpec")
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
}

// UnitTasksSynced says the unit cache answers.
func (g *Gate) UnitTasksSynced() bool { return g.unitsSynced.Load() }

// UnitTaskObjects is the unit TaskRuns by name, of one plan or ("") of all.
func (g *Gate) UnitTaskObjects(plan string) []unstructured.Unstructured {
	g.unitMu.RLock()
	defer g.unitMu.RUnlock()
	out := make([]unstructured.Unstructured, 0, len(g.units))
	for _, u := range g.units {
		if plan == "" || u.GetLabels()[g.UnitTaskLabel] == plan {
			out = append(out, *u.DeepCopy())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	return out
}

func (g *Gate) observeUnitTask(t watch.EventType, obj *unstructured.Unstructured) {
	g.unitMu.Lock()
	defer g.unitMu.Unlock()
	if t == watch.Deleted {
		delete(g.units, obj.GetName())
		return
	}
	slimUnitTask(obj)
	g.units[obj.GetName()] = obj
}

// listUnitTasks replaces the unit cache with a full list.
func (g *Gate) listUnitTasks(ctx context.Context) (string, error) {
	dyn, err := g.Dynamic()
	if err != nil {
		return "", err
	}
	res := dyn.Resource(TaskRunGVR).Namespace(g.Namespace)
	next := map[string]*unstructured.Unstructured{}
	cursors := map[string]bool{}
	options := metav1.ListOptions{LabelSelector: g.UnitTaskLabel, Limit: 100}
	for {
		page, err := res.List(ctx, options)
		if err != nil {
			return "", err
		}
		for i := range page.Items {
			obj := &page.Items[i]
			slimUnitTask(obj)
			next[obj.GetName()] = obj
		}
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			g.unitMu.Lock()
			g.units = next
			g.unitMu.Unlock()
			return page.GetResourceVersion(), nil
		}
		if cursors[options.Continue] {
			return "", fmt.Errorf("unit task run list repeated a cursor")
		}
		cursors[options.Continue] = true
	}
}

func (g *Gate) runUnitTasks(ctx context.Context) {
	g.followTaskRuns(ctx, "unit task runs", g.UnitTaskLabel, g.listUnitTasks, g.observeUnitTask, &g.unitsSynced)
}
