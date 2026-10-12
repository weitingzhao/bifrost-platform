package delivery

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Absent declarations are valid (a Pipeline may have no params/workspaces),
// but present declarations must be readable in the Kubernetes JSON shape.
func pipelineDeclarations(pipe *unstructured.Unstructured, field string) ([]any, error) {
	if pipe == nil {
		return nil, fmt.Errorf("pipeline declaration cannot be read: missing Pipeline")
	}
	spec, ok := pipe.Object["spec"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pipeline declaration spec cannot be read")
	}
	raw, present := spec[field]
	if !present {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("pipeline declaration %s cannot be read", field)
	}
	seen := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pipeline declaration %s cannot be read", field)
		}
		name, ok := m["name"].(string)
		if !ok || strings.TrimSpace(name) == "" || seen[name] {
			return nil, fmt.Errorf("pipeline declaration %s name cannot be read", field)
		}
		seen[name] = true
		if field == "params" {
			paramType := "string"
			if rawType, exists := m["type"]; exists {
				var ok bool
				paramType, ok = rawType.(string)
				if !ok || (paramType != "string" && paramType != "array" && paramType != "object") {
					return nil, fmt.Errorf("param %q type declaration cannot be read", name)
				}
			}
			if value, exists := m["default"]; exists && !readableParamDefault(paramType, value) {
				return nil, fmt.Errorf("param %q default declaration cannot be read", name)
			}
		}
		if field == "workspaces" {
			if value, exists := m["optional"]; exists {
				if _, ok := value.(bool); !ok {
					return nil, fmt.Errorf("workspace %q optional declaration cannot be read", name)
				}
			}
		}
	}
	return list, nil
}

func validateRunRequirements(pipe, run *unstructured.Unstructured) error {
	// Read both declarations before testing bindings so malformed data never
	// becomes an empty set of requirements.
	workspaces, err := pipelineDeclarations(pipe, "workspaces")
	if err != nil {
		return err
	}
	params, err := pipelineDeclarations(pipe, "params")
	if err != nil {
		return err
	}
	bound, _, err := unstructured.NestedSlice(run.Object, "spec", "workspaces")
	if err != nil {
		return fmt.Errorf("run workspaces cannot be read: %w", err)
	}
	supplied, _, err := unstructured.NestedSlice(run.Object, "spec", "params")
	if err != nil {
		return fmt.Errorf("run params cannot be read: %w", err)
	}
	names := func(items []any) map[string]bool {
		out := map[string]bool{}
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if name, ok := m["name"].(string); ok {
					out[name] = true
				}
			}
		}
		return out
	}
	bindings, values := names(bound), names(supplied)
	for _, item := range workspaces {
		m := item.(map[string]any)
		name := m["name"].(string)
		optional, _ := m["optional"].(bool)
		if !optional && !bindings[name] {
			return fmt.Errorf("required workspace %q is not bound", name)
		}
	}
	for _, item := range params {
		m := item.(map[string]any)
		name := m["name"].(string)
		_, hasDefault := m["default"]
		if !hasDefault && !values[name] {
			return fmt.Errorf("required param %q is not supplied", name)
		}
	}
	return nil
}

func readableParamDefault(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "array":
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case "object":
		values, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}
