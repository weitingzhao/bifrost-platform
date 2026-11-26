package delivery

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// kanikoExecutor is the image whose steps build container images. A pipeline
// that runs it must schedule on amd64 and passes the build preflight first
// (workload placement G2).
//
// Read from the pipeline's definition, not a hand-kept list (决定 2026-11-17,
// proposal B, duty/proposal-ci-w1-2026-11-17-kaniko-gate-list.md):
// the list had missed bifrost-build-research-pine and bifrost-ci-finance, both
// kaniko builds, and named bifrost-deliver-platform-prod, which builds nothing.
const kanikoExecutor = "kaniko-project/executor"

// taskGetter returns a Task object by name.
type taskGetter func(name string) (map[string]any, error)

// pipelineRunsKaniko says whether a Pipeline object runs the kaniko executor:
// in an inline taskSpec, or in a Task its taskRef names. A Task that cannot be
// read is an error unless another task already shows kaniko; the caller then
// treats the pipeline as a kaniko one (the stricter side).
func pipelineRunsKaniko(pipe map[string]any, getTask taskGetter) (bool, error) {
	spec, _ := pipe["spec"].(map[string]any)
	var unread []string
	for _, key := range []string{"tasks", "finally"} {
		for _, raw := range anyList(spec[key]) {
			task, _ := raw.(map[string]any)
			if ts, ok := task["taskSpec"].(map[string]any); ok && specRunsKaniko(ts) {
				return true, nil
			}
			ref, ok := task["taskRef"].(map[string]any)
			if !ok {
				continue
			}
			name, _ := ref["name"].(string)
			kind, _ := ref["kind"].(string)
			if resolver, _ := ref["resolver"].(string); resolver != "" || name == "" || (kind != "" && kind != "Task") {
				unread = append(unread, fmt.Sprintf("task %v: taskRef %v not read", task["name"], ref))
				continue
			}
			obj, err := getTask(name)
			if err != nil {
				if msg := fmt.Sprintf("Task %s: %v", name, err); !containsString(unread, msg) {
					unread = append(unread, msg)
				}
				continue
			}
			if ts, ok := obj["spec"].(map[string]any); ok && specRunsKaniko(ts) {
				return true, nil
			}
		}
	}
	if len(unread) > 0 {
		return false, fmt.Errorf("%s", strings.Join(unread, "; "))
	}
	return false, nil
}

// specRunsKaniko: a Task spec has an image (steps, sidecars, stepTemplate)
// naming the kaniko executor, directly or as a $(params.x) whose default does.
func specRunsKaniko(ts map[string]any) bool {
	defaults := map[string]string{}
	for _, p := range anyList(ts["params"]) {
		pm, _ := p.(map[string]any)
		if name, _ := pm["name"].(string); name != "" {
			if def, ok := pm["default"].(string); ok {
				defaults[name] = def
			}
		}
	}
	var found bool
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if img, ok := x["image"].(string); ok {
				img = strings.TrimSpace(img)
				if m := pipelineParamRef.FindStringSubmatch(img); m != nil {
					img = defaults[m[1]]
				}
				if strings.Contains(img, kanikoExecutor) {
					found = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	for _, key := range []string{"steps", "sidecars", "stepTemplate"} {
		walk(ts[key])
	}
	return found
}

// clusterTasks reads Tasks from the pipelines namespace, each at most once.
// The first lookup lists them all in one call: a Get per Task went through the
// client's rate limit (5 a second after a burst of 10), and the pipeline list
// spent 2.8 s of every request waiting on it (PROD, 2026-11-20). A name the
// list did not have is still read on its own, so a missing Task fails as before.
func (s *Service) clusterTasks(ctx context.Context, dyn dynamic.Interface) taskGetter {
	ns := s.PipelinesNamespace()
	seen := map[string]map[string]any{}
	failed := map[string]error{}
	listed := false
	return func(name string) (map[string]any, error) {
		if !listed {
			listed = true
			if list, err := dyn.Resource(taskGVR).Namespace(ns).List(ctx, metav1.ListOptions{}); err == nil {
				for i := range list.Items {
					seen[list.Items[i].GetName()] = list.Items[i].Object
				}
			}
		}
		if obj, ok := seen[name]; ok {
			return obj, nil
		}
		if err, ok := failed[name]; ok {
			return nil, err
		}
		obj, err := dyn.Resource(taskGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			failed[name] = err
			return nil, err
		}
		seen[name] = obj.Object
		return obj.Object, nil
	}
}

// kanikoOf decides whether the build preflight and the amd64 placement apply
// to a pipeline. A definition that was not read counts as kaniko, and unread
// says why, so a refusal can name it.
func kanikoOf(pipe map[string]any, getTask taskGetter) (kaniko bool, unread string) {
	uses, err := pipelineRunsKaniko(pipe, getTask)
	if err != nil {
		return true, "the pipeline definition was not read: " + err.Error()
	}
	return uses, ""
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
