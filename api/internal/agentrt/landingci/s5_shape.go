package landingci

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Tekton stores what it resolved, not the bytes the trusted contract was
// written in. Observed on cicd PipelineRuns, TaskRuns and Pods (Tekton
// v1.6.0; trimmed real objects in testdata/tekton-real-*.json):
//
//   - status.pipelineSpec / status.taskSpec carry $(params.*) and
//     $(context.pipelineRun.*) already substituted;
//   - every duration passes through metav1.Duration ("600s" -> "10m0s",
//     the run's "1200s" -> "20m0s");
//   - taskRef without kind is defaulted to kind: Task;
//   - the JSON round-trip through the Go types adds or drops zero values:
//     results description "", workspace readOnly false, step
//     computeResources {}, volumeClaimTemplate metadata.creationTimestamp
//     null and status {}.
//
// s5Tekton removes exactly these inert differences, on both sides, before
// the exact comparison. Everything else (images, commands, env, params,
// security contexts, mounts, task names, ordering) stays compared byte for
// byte; nothing here widens what a trusted value may be.
func s5Tekton(v any) any {
	return s5Normalize(s5Plain(v), "")
}

// s5Resolved compares a stored Tekton value with the contract value after
// the same normalization (and after the caller substituted parameters).
func s5Resolved(actual, contract any) bool {
	return s5Same(s5Tekton(actual), s5Tekton(contract))
}

// s5Plain deep-copies through JSON so maps, slices and numbers have the
// dynamic client's shape regardless of where the value came from.
func s5Plain(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func s5Normalize(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, value := range x {
			n := s5Normalize(value, k)
			switch {
			case k == "description" && n == "",
				k == "readOnly" && n == false,
				k == "creationTimestamp" && n == nil,
				k == "computeResources" && s5EmptyMap(n):
				continue
			}
			out[k] = n
		}
		switch key {
		case "taskRef":
			if _, ok := out["kind"]; !ok {
				out["kind"] = "Task"
			}
		case "volumeClaimTemplate":
			for _, k := range []string{"metadata", "status"} {
				if s5EmptyMap(out[k]) {
					delete(out, k)
				}
			}
		case "timeouts":
			for k, value := range out {
				out[k] = s5Duration(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = s5Normalize(value, "")
		}
		return out
	case string:
		if key == "timeout" {
			return s5Duration(x)
		}
		return x
	default:
		return x
	}
}

// s5Duration is metav1.Duration's encoding. An unparsable value is kept as
// is, so it can only fail the comparison.
func s5Duration(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return v
	}
	return d.String()
}

func s5EmptyMap(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

// s5Substitute applies Tekton's string substitution of $(params.<name>) and
// the given context variables to a contract value, as the controller does
// before it stores status.pipelineSpec / status.taskSpec.
func s5Substitute(v any, params map[string]string, context map[string]string) any {
	pairs := make([]string, 0, 2*(len(params)+len(context)))
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pairs = append(pairs, "$(params."+name+")", params[name])
	}
	names = names[:0]
	for name := range context {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pairs = append(pairs, "$(context."+name+")", context[name])
	}
	r := strings.NewReplacer(pairs...)
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, value := range x {
				out[k] = walk(value)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, value := range x {
				out[i] = walk(value)
			}
			return out
		case string:
			return r.Replace(x)
		default:
			return x
		}
	}
	return walk(s5Plain(v))
}

// s5TaskRef: a stored TaskRun taskRef names exactly this Task (kind
// defaulted to Task; no resolver, bundle or other kind).
func s5TaskRef(ref map[string]any, name string) bool {
	return s5Resolved(map[string]any{"taskRef": ref}, map[string]any{"taskRef": map[string]any{"name": name}})
}

// s5TaskRunTemplate is what Tekton v1.6.0 runs a pipeline task with
// (PipelineRunSpec.GetTaskRunSpec): the task's taskRunSpecs entry over
// taskRunTemplate, its podTemplate merged field by field over the default
// one (pod.MergePodTemplateWithDefault: a field the task does not set keeps
// the default). Only the release freeze check has an entry.
func s5TaskRunTemplate(contract s5Contract, pipelineTask string) (string, map[string]any) {
	base, _ := contract.RunTemplate["taskRunTemplate"].(map[string]any)
	sa, _ := base["serviceAccountName"].(string)
	pod := map[string]any{}
	if def, ok := base["podTemplate"].(map[string]any); ok {
		for k, v := range def {
			pod[k] = v
		}
	}
	for _, v := range sliceS5(contract.RunTemplate["taskRunSpecs"]) {
		spec, _ := v.(map[string]any)
		if spec["pipelineTaskName"] != pipelineTask {
			continue
		}
		if name, ok := spec["serviceAccountName"].(string); ok && name != "" {
			sa = name
		}
		if override, ok := spec["podTemplate"].(map[string]any); ok {
			for k, v := range override {
				pod[k] = v
			}
		}
	}
	return sa, pod
}

// s5PipelineTask returns the contract's pipeline task by name.
func s5PipelineTask(contract s5Contract, name string) map[string]any {
	for _, v := range sliceS5(contract.Pipeline["tasks"]) {
		if task, _ := v.(map[string]any); task != nil && task["name"] == name {
			return task
		}
	}
	return nil
}
