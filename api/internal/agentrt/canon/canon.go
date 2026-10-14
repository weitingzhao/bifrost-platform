// Package canon encodes JSON the way the infra event recorder does, so an
// event hashed here and one hashed by the recorder agree byte for byte.
//
// The recorder writes Python's json.dumps(obj, sort_keys=True,
// separators=(",", ":"), ensure_ascii=False). encoding/json differs from it in
// three places: it escapes <, > and &, it escapes U+2028 and U+2029, and it
// formats floats differently. This encoder writes the Python form and accepts
// only what both sides encode the same way: objects, arrays, strings, booleans,
// null and integers that fit in an int64. A float, a duplicate key or invalid
// UTF-8 is an error, never a silent rewrite.
package canon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

var (
	ErrFloat     = errors.New("canon: only integers are allowed")
	ErrDuplicate = errors.New("canon: duplicate object key")
	ErrUTF8      = errors.New("canon: string is not valid UTF-8")
	ErrTrailing  = errors.New("canon: trailing data after the JSON value")
)

// Marshal returns the canonical bytes of a JSON document given as bytes.
func Marshal(doc []byte) ([]byte, error) {
	v, err := Parse(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encode returns the canonical bytes of a value built from map[string]any,
// []any, string, bool, nil, json.Number and Go integer types.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Sum returns the lowercase hex sha256 of the canonical bytes of v.
func Sum(v any) (string, error) {
	b, err := Encode(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Parse decodes one JSON document, refusing duplicate keys, invalid UTF-8 and
// trailing data. Numbers stay json.Number so integers are not rounded.
func Parse(doc []byte) (any, error) {
	if !utf8.Valid(doc) {
		return nil, ErrUTF8
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, ErrTrailing
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := map[string]any{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("canon: object key %v", kt)
				}
				if _, dup := obj[k]; dup {
					return nil, fmt.Errorf("%w: %q", ErrDuplicate, k)
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj[k] = v
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("canon: unexpected %v", t)
	default:
		return tok, nil
	}
}

func encode(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		return encodeString(buf, t)
	case json.Number:
		n, err := strconv.ParseInt(string(t), 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFloat, t)
		}
		buf.WriteString(strconv.FormatInt(n, 10))
	case int:
		buf.WriteString(strconv.FormatInt(int64(t), 10))
	case int32:
		buf.WriteString(strconv.FormatInt(int64(t), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case uint32:
		buf.WriteString(strconv.FormatUint(uint64(t), 10))
	case float32, float64:
		return ErrFloat
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encode(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case []string:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeString(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// Python sorts str keys by code point; for valid UTF-8 that is the
		// byte order Go's string comparison uses.
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := encode(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canon: unsupported type %T", v)
	}
	return nil
}

// encodeString follows Python's json with ensure_ascii=False: escape the
// quote, the backslash and every character below U+0020; write everything
// else, including U+2028 and U+2029, as UTF-8.
func encodeString(buf *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return ErrUTF8
	}
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, c)
			} else {
				buf.WriteByte(c)
			}
		}
	}
	buf.WriteByte('"')
	return nil
}
