package notice

import (
	"encoding/json"
	"unicode/utf8"
)

// HiddenJSONFields reports object keys whose JSON string value is not valid
// UTF-8 or contains an unpaired surrogate escape. json.Unmarshal replaces
// those with U+FFFD before a Go string exists, so the check reads the raw
// literal. Nested objects are keyed as parent.child. A non-object body or
// invalid UTF-8 in the whole document sets "*".
func HiddenJSONFields(body []byte) map[string]bool {
	out := map[string]bool{}
	if len(body) == 0 {
		return out
	}
	if !utf8.Valid(body) {
		out["*"] = true
		return out
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return out
	}
	walkJSON(out, "", obj)
	return out
}

func walkJSON(out map[string]bool, prefix string, obj map[string]json.RawMessage) {
	for k, v := range obj {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		if len(v) > 0 && v[0] == '{' {
			var child map[string]json.RawMessage
			if json.Unmarshal(v, &child) == nil {
				walkJSON(out, name, child)
				continue
			}
		}
		if jsonLiteralHidden(v) {
			out[name] = true
		}
	}
}

func jsonLiteralHidden(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] != '"' {
		return false
	}
	if !utf8.Valid(raw) || unpairedSurrogate(string(raw)) {
		return true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return true
	}
	return HasHidden(s)
}

func unpairedSurrogate(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if s[i] != '\\' || s[i+1] != 'u' {
			continue
		}
		r, ok := hex4(s[i+2:])
		if !ok || r < 0xD800 || r > 0xDFFF {
			continue
		}
		if r <= 0xDBFF && i+12 <= len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
			low, ok := hex4(s[i+8:])
			if ok && low >= 0xDC00 && low <= 0xDFFF {
				i += 11
				continue
			}
		}
		return true
	}
	return false
}

func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var n rune
	for i := 0; i < 4; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			n = n*16 + rune(c-'0')
		case c >= 'a' && c <= 'f':
			n = n*16 + rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n*16 + rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return n, true
}
