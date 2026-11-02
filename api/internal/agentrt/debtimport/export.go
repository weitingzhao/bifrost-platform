package debtimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Layout stores syntax, closed-set labels and pointers, never source prose or
// value baselines.
// Byte ranges identify source ownership for the independently checked report.
type Layout struct {
	Ledger   []Fragment `json:"ledger"`
	Ratchets []Fragment `json:"ratchets"`
}
type Fragment struct {
	Syntax string   `json:"syntax,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Render string   `json:"render,omitempty"`
	Start  int      `json:"start"`
	End    int      `json:"end"`
}
type span struct {
	start, end int
	paths      []string
	render     string
}

func (p *parser) bind(doc, start, end int, paths []string, render string) {
	if end > start {
		p.spans[doc] = append(p.spans[doc], span{start, end, paths, render})
	}
}
func projection(r Request) (map[string]any, error) {
	r.Markdown = Layout{}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	err = json.Unmarshal(data, &root)
	return root, err
}
func valuesAt(root map[string]any, paths []string) ([]any, error) {
	values := []any{}
	for _, path := range paths {
		if !strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("invalid export binding %q", path)
		}
		var value any = root
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		for j, key := range parts {
			switch v := value.(type) {
			case map[string]any:
				var ok bool
				value, ok = v[key]
				if !ok && !(j == len(parts)-1 && key == "watch_until") {
					return nil, fmt.Errorf("export binding %q no longer exists", path)
				}
			case []any:
				i, err := strconv.Atoi(key)
				if err != nil || i < 0 || i >= len(v) {
					return nil, fmt.Errorf("export binding %q is out of range", path)
				}
				value = v[i]
			default:
				return nil, fmt.Errorf("invalid export binding %q", path)
			}
		}
		values = append(values, value)
	}
	return values, nil
}

// Export returns the two source-shaped documents. The caller selects their
// destination; this function never writes files. Requests without presentation
// metadata are rejected rather than inventing missing source fields.
func Export(r Request) (ledger, ratchets []byte, err error) {
	if r.Format != Format {
		return nil, nil, fmt.Errorf("unsupported format %q", r.Format)
	}
	if len(r.Markdown.Ledger) == 0 && len(r.Markdown.Ratchets) == 0 {
		return nil, nil, fmt.Errorf("request has no Markdown presentation metadata")
	}
	root, err := projection(r)
	if err != nil {
		return nil, nil, err
	}
	ledger, err = renderDocument(root, r.Markdown.Ledger)
	if err != nil {
		return nil, nil, err
	}
	ratchets, err = renderDocument(root, r.Markdown.Ratchets)
	return ledger, ratchets, err
}
func renderDocument(root map[string]any, fragments []Fragment) ([]byte, error) {
	var b bytes.Buffer
	for _, f := range fragments {
		if len(f.Paths) == 0 {
			if !layoutOnly(f.Syntax) {
				return nil, fmt.Errorf("non-layout literal at byte %d", f.Start)
			}
			b.WriteString(f.Syntax)
			continue
		}
		if f.Syntax != "" {
			return nil, fmt.Errorf("field binding also contains literal syntax")
		}
		values, err := valuesAt(root, f.Paths)
		if err != nil {
			return nil, err
		}
		v, err := renderValue(f.Render, values)
		if err != nil {
			return nil, err
		}
		b.WriteString(v)
	}
	return b.Bytes(), nil
}
func renderValue(kind string, values []any) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("empty export binding")
	}
	s, ok := values[0].(string)
	if !ok {
		return "", fmt.Errorf("non-text value in %s binding", kind)
	}
	switch kind {
	case "repos", "gates", "same":
		parts := []string{}
		for _, v := range values {
			part, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("non-text %s binding", kind)
			}
			parts = append(parts, part)
		}
		if kind != "repos" {
			for _, part := range parts {
				if part != parts[0] {
					return "", fmt.Errorf("one legacy field cannot express differing record values")
				}
			}
			return parts[0], nil
		}
		return strings.Join(parts, ", "), nil
	case "string":
		return s, nil
	case "cell":
		return s, nil
	case "state":
		label := stateLabel(s)
		if s == "watching" && len(values) > 1 && values[1] != nil {
			t, err := time.Parse(time.RFC3339, fmt.Sprint(values[1]))
			if err != nil {
				return "", err
			}
			zone, _ := time.LoadLocation("America/Chicago")
			label += "（到 " + t.In(zone).Format("01-02") + "）"
		}
		return label, nil
	default:
		return "", fmt.Errorf("unknown export renderer %q", kind)
	}
}
func stateLabel(s string) string {
	if label, ok := map[string]string{"open": "未开始", "doing": "在做", "watching": "观察中", "awaiting_signoff": "待你签收", "closed": "已关闭"}[s]; ok {
		return label
	}
	return s
}
