package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Configuration boundaries are projections of Git and the installed Tekton
// objects, never another configuration authority. A proposed change is an
// ordinary delivery goal; observing its requested value is a separate read.
type configBoundary struct {
	id, file, kind, name, unit string
	path                       []string
	scriptTimeout              bool
}

var configurationBoundaries = []configBoundary{
	{"ci.platform.go_timeout", "task-ci-platform-test-api.yaml", "tasks", "bifrost-ci-platform-test-api", "duration", []string{"spec", "steps", "@go-test", "script"}, true},
	{"ci.platform.console_timeout", "pipeline-ci-platform.yaml", "pipelines", "bifrost-ci-platform", "duration", []string{"spec", "tasks", "@test-console", "timeout"}, false},
	{"ci.platform.api_cpu_limit", "task-ci-platform-test-api.yaml", "tasks", "bifrost-ci-platform-test-api", "cpu", []string{"spec", "steps", "@go-test", "computeResources", "limits", "cpu"}, false},
	{"ci.platform.api_memory_limit", "task-ci-platform-test-api.yaml", "tasks", "bifrost-ci-platform-test-api", "memory", []string{"spec", "steps", "@go-test", "computeResources", "limits", "memory"}, false},
	{"ci.infra.cpu_limit", "task-ci-infra-check.yaml", "tasks", "bifrost-ci-infra-check", "cpu", []string{"spec", "steps", "@tools", "computeResources", "limits", "cpu"}, false},
	{"ci.infra.memory_limit", "task-ci-infra-check.yaml", "tasks", "bifrost-ci-infra-check", "memory", []string{"spec", "steps", "@tools", "computeResources", "limits", "memory"}, false},
}

var goTimeout = regexp.MustCompile(`-timeout=([0-9]+(?:\.[0-9]+)?[a-z]+)`)
var sharedGoTimeout = regexp.MustCompile(`(?m)^\s*export BIFROST_GO_TEST_TIMEOUT=([^\s]+)\s*$`)

// configNode accepts only a registry selector. Array members are selected by
// name, not their incidental position; a missing or duplicated member refuses.
func configNode(n *yaml.Node, path []string) (*yaml.Node, error) {
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	for _, part := range path {
		var found *yaml.Node
		if strings.HasPrefix(part, "@") && n.Kind == yaml.SequenceNode {
			for _, item := range n.Content {
				name, err := configNode(item, []string{"name"})
				if err == nil && name.Value == part[1:] {
					if found != nil {
						return nil, fmt.Errorf("duplicate selector %s", part)
					}
					found = item
				}
			}
		} else if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == part {
					if found != nil {
						return nil, fmt.Errorf("duplicate field %s", part)
					}
					found = n.Content[i+1]
				}
			}
		}
		if found == nil {
			return nil, fmt.Errorf("missing selector %s", part)
		}
		n = found
	}
	return n, nil
}

func (b configBoundary) read(text []byte) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return "", err
	}
	n, err := configNode(&doc, b.path)
	if err != nil {
		return "", err
	}
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("expected a scalar")
	}
	if !b.scriptTimeout {
		return n.Value, nil
	}
	shared := sharedGoTimeout.FindAllStringSubmatch(n.Value, -1)
	if len(shared) > 0 {
		if len(shared) != 1 || strings.Count(n.Value, "-timeout=") != strings.Count(n.Value, `-timeout="${BIFROST_GO_TEST_TIMEOUT}"`) {
			return "", fmt.Errorf("Go test timeout sources conflict")
		}
		value := shared[0][1]
		if err := b.validate(value, nil); err != nil {
			return "", err
		}
		return value, nil
	}
	if strings.Contains(n.Value, "scripts/ci/test_api_scoped.sh") {
		return "", fmt.Errorf("scoped tests have no shared timeout; literal fallback values do not establish the effective timeout")
	}
	matches := goTimeout.FindAllStringSubmatch(n.Value, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("no explicit Go test timeout")
	}
	v := matches[0][1]
	for _, m := range matches {
		if m[1] != v {
			return "", fmt.Errorf("Go test branches use different timeouts")
		}
	}
	return v, nil
}

func (b configBoundary) validate(value string, source []byte) error {
	if b.unit == "duration" {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("enter a positive duration, such as 15m")
		}
		return nil
	}
	q, err := resource.ParseQuantity(value)
	if err != nil || q.Sign() <= 0 {
		return fmt.Errorf("enter a positive Kubernetes %s quantity", b.unit)
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(source, &doc); err != nil {
		return err
	}
	p := append([]string{}, b.path...)
	p[len(p)-2] = "requests"
	if n, e := configNode(&doc, p); e == nil {
		request, e := resource.ParseQuantity(n.Value)
		if e != nil {
			return e
		}
		if q.Cmp(request) < 0 {
			return fmt.Errorf("limit is below the declared request %s", n.Value)
		}
	}
	return nil
}

func (b configBoundary) equal(a, c string) bool {
	if b.unit == "duration" {
		x, e := time.ParseDuration(a)
		y, f := time.ParseDuration(c)
		return e == nil && f == nil && x == y
	}
	x, e := resource.ParseQuantity(a)
	y, f := resource.ParseQuantity(c)
	return e == nil && f == nil && x.Cmp(y) == 0
}

// ConfigurationBoundaries reads only the registered non-secret fields. No
// manifest, CI or goal is written by this method, including preview requests.
func (h *Handler) ConfigurationBoundaries(ctx context.Context, key, value string, requests []map[string]any) (map[string]any, error) {
	f := h.PolicyFacts()
	head, err := f.MainHead(ctx, "bifrost-trade-infra")
	if err != nil {
		return nil, fmt.Errorf("configuration source revision unavailable")
	}
	dyn, liveErr := f.s.buildDynamicClient()
	sources := map[string][]byte{}
	installed := map[string][]byte{}
	sourceErrors := map[string]string{}
	liveErrors := map[string]string{}
	rows := []map[string]any{}
	var preview map[string]any
	for _, b := range configurationBoundaries {
		path := "k8s/cicd/tekton/" + b.file
		if _, ok := sources[path]; !ok && sourceErrors[path] == "" {
			text, found, e := f.FileAt(ctx, "bifrost-trade-infra", head, path)
			if e != nil || !found {
				sourceErrors[path] = "Git source could not be read"
			} else {
				sources[path] = []byte(text)
			}
		}
		object := b.kind + "/" + b.name
		if _, ok := installed[object]; !ok && liveErrors[object] == "" {
			if liveErr != nil {
				liveErrors[object] = "Kubernetes reader unavailable"
			} else {
				obj, e := dyn.Resource(schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: b.kind}).Namespace(f.s.PipelinesNamespace()).Get(ctx, b.name, metav1.GetOptions{})
				if e != nil {
					liveErrors[object] = "Installed object could not be read"
				} else {
					installed[object], e = json.Marshal(obj.Object)
					if e != nil {
						return nil, e
					}
				}
			}
		}
		source, sourceErr := b.read(sources[path])
		live, readErr := b.read(installed[object])
		row := map[string]any{"boundary_id": b.id, "repo": "bifrost-trade-infra", "revision": head, "path": path, "selector": strings.Join(b.path, "/"), "unit": b.unit, "object": object, "source_value": nil, "installed_value": nil, "state": "unknown", "adjustment": "versioned_configuration"}
		if sourceErr == nil {
			row["source_value"] = source
		} else {
			row["source_error"] = "Registered selector unavailable in Git"
		}
		if readErr == nil {
			row["installed_value"] = live
		} else {
			row["installed_error"] = "Installed value is unknown"
		}
		if sourceErrors[path] != "" {
			row["source_error"] = sourceErrors[path]
		}
		if liveErrors[object] != "" {
			row["installed_error"] = liveErrors[object]
		}
		if sourceErr == nil && readErr == nil {
			if b.equal(source, live) {
				row["state"] = "source_matches_installed"
			} else {
				row["state"] = "source_differs_from_installed"
			}
		}
		rows = append(rows, row)
		if key == b.id {
			if sourceErr != nil {
				return nil, fmt.Errorf("configuration source selector unavailable")
			}
			value = strings.TrimSpace(value)
			if e := b.validate(value, sources[path]); e != nil {
				return nil, e
			}
			if b.equal(source, value) {
				return nil, fmt.Errorf("Git already contains the requested value; no configuration change is needed. Check the installed value and the existing deployment path")
			}
			request := map[string]string{"boundary_id": b.id, "value": value, "source_revision": head, "previous_value": source}
			encoded, _ := json.Marshal(request)
			goal := fmt.Sprintf("Delivery configuration: %s\n%s\nChange only %s (%s) from %s to %s, based on infra %s. Preserve other settings and comments. For script timeouts update every Go test branch consistently. Validate the exact requested value, then use the normal landing and deployment channel. Re-read the installed Tekton object after deployment; a commit or accepted task alone is not completion. If the source value changed, report the conflict instead of overwriting it.", b.id, encoded, path, strings.Join(b.path, "/"), source, value, head)
			preview = map[string]any{"boundary_id": key, "requested_value": value, "previous_value": source, "source_revision": head, "goal": goal, "repo": "bifrost-trade-infra"}
		}
	}
	if key != "" && preview == nil {
		return nil, fmt.Errorf("unregistered configuration boundary")
	}
	for _, request := range requests {
		request["delivery_state"] = "unknown"
		wanted, ok := request["request"].(map[string]string)
		if !ok {
			continue
		}
		for i, boundary := range configurationBoundaries {
			if boundary.id != wanted["boundary_id"] {
				continue
			}
			installed, installedOK := rows[i]["installed_value"].(string)
			source, sourceOK := rows[i]["source_value"].(string)
			switch {
			case installedOK && boundary.equal(installed, wanted["value"]):
				request["delivery_state"] = "requested_value_observed"
			case sourceOK && installedOK && boundary.equal(source, wanted["value"]):
				request["delivery_state"] = "awaiting_deployment"
			case sourceOK && installedOK:
				request["delivery_state"] = "awaiting_source_change"
			}
		}
	}
	return map[string]any{"as_of": time.Now().UTC(), "repo": "bifrost-trade-infra", "revision": head, "rows": rows, "requests": requests, "preview": preview, "note": "Installed definitions affect new runs. Existing PipelineRuns retain their resolved specifications. Requested-value observation does not attribute a deployment to a particular goal."}, nil
}
