package questions

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// clean removes controls, bidi marks, zero-width characters, and U+2028/U+2029,
// masks secret shapes, then trims. body and answer text keep newlines, which
// are normalized to \n. Every other field loses newlines with the other controls.
func clean(s string, keepNewlines bool) string {
	s = notice.StripControls(s, keepNewlines)
	s = notice.Redact(s)
	if keepNewlines {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return strings.TrimSpace(s)
}

// encodedBytes is the JSON encoding of s with no HTML escape, quotes included.
func encodedBytes(s string) int {
	raw, err := marshalJSON(s)
	if err != nil {
		return len(s) + 2
	}
	return len(raw)
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// clipEncoded cleans s the way a single-line question field is cleaned, then
// keeps the longest prefix whose JSON encoding is at most n bytes (quotes included).
func clipEncoded(s string, n int) string {
	s = clean(s, false)
	if encodedBytes(s) <= n {
		return s
	}
	r := []rune(s)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if encodedBytes(string(r[:mid])) <= n {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(r[:lo])
}

func clipPrincipal(s string) string {
	return clipEncoded(s, principalLimit)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
