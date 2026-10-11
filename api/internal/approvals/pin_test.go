package approvals

import "testing"

// TestPinEscapeVisible records escapeVisible as it behaves today. The
// expected strings are the contract for the later move into notice: they
// stay, and the function stays its own function.
func TestPinEscapeVisible(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"c0", "\u0007", `\u0007`},
		{"u202e", "\u202e", `\u202e`},
		{"u200b", "\u200b", "\u200b"},
		{"newline", "\n", `\n`},
		{"secret", "token=s3cr3tvalue", "token=s3cr3tvalue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeVisible(tc.in); got != tc.want {
				t.Fatalf("escapeVisible(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
