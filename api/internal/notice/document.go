package notice

import "strings"

// redactedText replaces a value kept under a key that names a secret, whatever
// JSON type that value has.
const redactedText = "[redacted]"

// A key that contains one of these, in any case, holds a secret. The match is
// on the key, so a value that does not look like a bearer is still replaced.
var sensitiveKeyParts = []string{
	"token", "secret", "password", "passwd", "authorization",
	"api_key", "apikey", "credential", "cookie",
}

func keySensitive(key string) bool {
	folded := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(folded, part) {
			return true
		}
	}
	return false
}

// RedactDocument walks a decoded JSON value the way a progress document is
// stored. Strings and object keys pass through Redact. force replaces the
// whole value: it is set only for a child of a sensitive key, so a number,
// boolean, array, object or null under that key is stored as [redacted] and
// is not walked.
func RedactDocument(v any, force bool) any {
	if force {
		return redactedText
	}
	switch t := v.(type) {
	case string:
		return Redact(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[Redact(k)] = RedactDocument(child, keySensitive(k))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = RedactDocument(child, false)
		}
		return out
	default:
		return v
	}
}
