package s5check

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// CanonicalJSON sorts object keys, preserves array order and emits UTF-8
// without whitespace or HTML escaping. Integer evidence fields retain all
// uint64 digits (including generation); they are never rounded via float64.
func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := canonicalValue(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func canonicalString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

func canonicalValue(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		canonicalString(b, x)
	case json.Number:
		s := string(x)
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			if s == "-0" {
				s = "0"
			}
			b.WriteString(s)
			return nil
		}
		if _, err := strconv.ParseUint(s, 10, 64); err == nil {
			b.WriteString(s)
			return nil
		}
		f, err := x.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("non-finite canonical number")
		}
		if f == 0 {
			b.WriteByte('0')
			return nil
		}
		if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
			b.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
			return nil
		}
		s = strconv.FormatFloat(f, 'e', -1, 64)
		parts := strings.Split(s, "e")
		exponent, _ := strconv.Atoi(parts[1])
		b.WriteString(parts[0])
		b.WriteByte('e')
		if exponent >= 0 {
			b.WriteByte('+')
		}
		b.WriteString(strconv.Itoa(exponent))
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i != 0 {
				b.WriteByte(',')
			}
			if err := canonicalValue(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, c := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for k := 0; k < len(a) && k < len(c); k++ {
				if a[k] != c[k] {
					return a[k] < c[k]
				}
			}
			return len(a) < len(c)
		})
		b.WriteByte('{')
		for i, key := range keys {
			if i != 0 {
				b.WriteByte(',')
			}
			canonicalString(b, key)
			b.WriteByte(':')
			if err := canonicalValue(b, x[key]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical value %T", v)
	}
	return nil
}

func SHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func Digest(value any) (string, error) {
	raw, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return SHA256(raw), nil
}
