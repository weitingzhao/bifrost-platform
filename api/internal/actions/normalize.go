package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Param is one argument of an action, returned by GET /api/v1/actions.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	In       string `json:"in,omitempty"`
}

// Normalize reduces params to the catalog shape used for params_hash.
// Absent optional booleans become false. Empty optional strings are dropped.
// Passthrough actions keep every key (the plugin heal body).
func (a Action) Normalize(in map[string]any) (map[string]any, error) {
	if in == nil {
		in = map[string]any{}
	}
	if a.Passthrough {
		return cloneMap(in), nil
	}
	out := map[string]any{}
	for _, p := range a.Params {
		v, ok := in[p.Name]
		if !ok || v == nil {
			if p.Type == "boolean" && !p.Required {
				out[p.Name] = false
			}
			continue
		}
		c, keep, err := coerce(p, v)
		if err != nil {
			return nil, err
		}
		if !keep {
			if p.Type == "boolean" && !p.Required {
				out[p.Name] = false
			}
			continue
		}
		out[p.Name] = c
	}
	return out, nil
}

// Missing returns the first required param that is absent.
func (a Action) Missing(params map[string]any) string {
	for _, p := range a.Params {
		if !p.Required {
			continue
		}
		v, ok := params[p.Name]
		if !ok || v == nil {
			return p.Name
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			return p.Name
		}
	}
	return ""
}

// Extract merges a direct-call request into the same shape Create stores.
func (a Action) Extract(r *http.Request, body []byte) (map[string]any, error) {
	merged := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &merged); err != nil {
			return nil, err
		}
	}
	if merged == nil {
		merged = map[string]any{}
	}
	if r != nil {
		for _, p := range a.Params {
			switch p.In {
			case "path":
				v := strings.TrimSpace(chi.URLParam(r, p.Name))
				// Chi names the catch-all on /plugins/market-data/api/* as "*".
				if v == "" && p.Name == "path" {
					v = strings.Trim(strings.TrimSpace(chi.URLParam(r, "*")), "/")
				}
				if v != "" {
					merged[p.Name] = v
				}
			case "query":
				if v := strings.TrimSpace(r.URL.Query().Get(p.Name)); v != "" {
					merged[p.Name] = v
				}
			}
		}
	}
	return a.Normalize(merged)
}

func coerce(p Param, v any) (any, bool, error) {
	switch p.Type {
	case "boolean":
		b, ok := asBool(v)
		if !ok {
			return nil, false, fmt.Errorf("param %s must be a boolean", p.Name)
		}
		return b, true, nil
	case "integer":
		n, ok := asInt32(v)
		if !ok {
			return nil, false, fmt.Errorf("param %s must be an integer", p.Name)
		}
		if !p.Required && n == 0 {
			return nil, false, nil
		}
		return int(n), true, nil
	case "string":
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			return "", false, nil
		}
		return s, true, nil
	case "string[]":
		ss, ok := asStrings(v)
		if !ok {
			return nil, false, fmt.Errorf("param %s must be an array of strings", p.Name)
		}
		if len(ss) == 0 {
			return nil, false, nil
		}
		return ss, true, nil
	default:
		return v, true, nil
	}
}

func asBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func asStrings(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		out := append([]string(nil), t...)
		return out, true
	case []any:
		out := make([]string, 0, len(t))
		for _, el := range t {
			s, ok := el.(string)
			if !ok {
				return nil, false
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}
