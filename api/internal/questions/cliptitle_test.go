package questions

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// ClipTitle cuts by the rule the store checks titles with: cleaned, JSON
// with its quotes, HTML not escaped.
func TestClipTitle(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int // encoded bytes at most titleLimit
	}{
		{strings.Repeat("a", 298), 300},
		{strings.Repeat("a", 299), 300},
		{strings.Repeat("&", 400), 300},
		{strings.Repeat("计", 200), 299},
		{"a\u0007b" + strings.Repeat("x", 400), 300},
	} {
		got := ClipTitle(c.in)
		if n := encodedBytes(got); n > titleLimit || n < c.want || !utf8.ValidString(got) {
			t.Errorf("%q…: %d encoded bytes", c.in[:3], n)
		}
		if _, err := buildTitleOnly(got); err != nil {
			t.Errorf("%q…: the store refuses the cut title: %v", c.in[:3], err)
		}
	}
	if got := ClipTitle(strings.Repeat("a", 299)); len(got) != 298 {
		t.Errorf("301 encoded bytes cut to %d characters; want 298", len(got))
	}
}

// buildTitleOnly checks a title as the store does when it builds a question.
func buildTitleOnly(title string) (string, error) {
	if err := limit(clean(title, false), "title", titleLimit); err != nil {
		return "", err
	}
	return title, nil
}
