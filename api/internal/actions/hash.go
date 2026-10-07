package actions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// ParamsHash is the hex sha256 of the canonical JSON of params.
// Object keys are sorted. Whole numbers encode as integers, so a value that
// survived a JSON round-trip (int → float64) hashes the same.
func ParamsHash(params map[string]any) (string, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, err := canonical(params)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func canonical(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := canonical(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, el := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			b, err := canonical(el)
			if err != nil {
				return nil, err
			}
			buf.Write(b)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case json.Number:
		return []byte(t.String()), nil
	case float32:
		return canonical(float64(t))
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, fmt.Errorf("non-finite number")
		}
		if t == math.Trunc(t) && t >= math.MinInt64 && t <= math.MaxInt64 {
			return []byte(strconv.FormatInt(int64(t), 10)), nil
		}
		return json.Marshal(t)
	case int:
		return []byte(strconv.Itoa(t)), nil
	case int32:
		return []byte(strconv.FormatInt(int64(t), 10)), nil
	case int64:
		return []byte(strconv.FormatInt(t, 10)), nil
	case uint:
		return []byte(strconv.FormatUint(uint64(t), 10)), nil
	case uint32:
		return []byte(strconv.FormatUint(uint64(t), 10)), nil
	case uint64:
		return []byte(strconv.FormatUint(t, 10)), nil
	default:
		return json.Marshal(v)
	}
}
