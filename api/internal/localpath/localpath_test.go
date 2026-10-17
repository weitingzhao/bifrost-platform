package localpath

import "testing"

func TestFind(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"see /home/u/x.go:3", true},
		{"/Users/a/b", true},
		{"/private/var/x", true},
		{"/var/folders/ab/T/x", true},
		{"/tmp/go-build1/x", true},
		{"~/go/pkg", true},
		{`C:\work`, true},
		{"(D:/x)", true},
		{"http://example.com/a", false},
		{"foo:/bar", false},
		{"./console/src/a.ts", false},
		{"../bifrost-ui/src", false},
		{"$HOME/go/pkg", false},
	} {
		if _, got := Find([]byte(c.in)); got != c.want {
			t.Errorf("Find(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMaskLeavesNothingFindMatches(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/home/u/agent-work/x.ts:3:1", "<home>/u/agent-work/x.ts:3:1"},
		{"/Users/bifrost-agentd/x", "<Users>/bifrost-agentd/x"},
		{"/private/var/folders/x", "<private><var-folders>/x"},
		{"/private/tmp/a", "<private><tmp>/a"},
		{"at /var/folders/ab/T/x", "at <var-folders>/ab/T/x"},
		{"/tmp/go-build1/b001", "<tmp>/go-build1/b001"},
		{"~/go/pkg/mod", "<~>/go/pkg/mod"},
		{`C:\work\a`, `<C:>\work\a`},
		{"(D:/x)", "(<D:>/x)"},
		{"/home//home/u", "<home>/<home>/u"},
		{"no path here", "no path here"},
		{"http://example.com/tmp", "http://example.com/tmp"},
	} {
		got := Mask(c.in)
		if got != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.in, got, c.want)
		}
		if p, found := Find([]byte(got)); found {
			t.Errorf("Mask(%q) = %q still holds a local path: %q", c.in, got, p)
		}
	}
}

func TestMaskLongChains(t *testing.T) {
	in := ""
	for range 50 {
		in += "/private/tmp"
	}
	in += "/x"
	if p, found := Find([]byte(Mask(in))); found {
		t.Fatalf("a long chain still holds a local path after Mask: %q", p)
	}
}
