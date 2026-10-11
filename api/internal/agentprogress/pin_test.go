package agentprogress

import (
	"reflect"
	"testing"
)

// TestPinDocumentCleaning records redactNested as it behaves today. A string
// is Redact. A sensitive key replaces the child. The expected values stay
// when the walk moves into notice.
func TestPinDocumentCleaning(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"c0", "\u0007", "\u0007"},
		{"u202e", "\u202e", "\u202e"},
		{"u200b", "\u200b", "\u200b"},
		{"newline", "\n", "\n"},
		{"secret", "token=s3cr3tvalue", "token=[redacted]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := redactNested(tc.in, false).(string)
			if !ok || got != tc.want {
				t.Fatalf("redactNested(%q, false) = %#v, want %q", tc.in, redactNested(tc.in, false), tc.want)
			}
		})
	}

	if got := redactNested("token=s3cr3tvalue", true); got != "[redacted]" {
		t.Fatalf("forced value = %#v", got)
	}

	got := redactNested(map[string]any{
		"note":                "token=s3cr3tvalue",
		"token":               map[string]any{"session": "abc"},
		"ok":                  "a\u0007b\u202ec\u200bd\n",
		"api_key=s3cr3tvalue": "x",
	}, false)
	want := map[string]any{
		"note":               "token=[redacted]",
		"token":              "[redacted]",
		"ok":                 "a\u0007b\u202ec\u200bd\n",
		"api_key=[redacted]": "[redacted]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("document =\n%#v\nwant\n%#v", got, want)
	}
}
