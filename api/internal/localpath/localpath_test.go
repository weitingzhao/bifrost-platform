package localpath

import (
	"encoding/json"
	"strings"
	"testing"
)

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

// Review 237 A: the encoder's escapes can complete H8's drive-letter form in
// strings Mask left alone. MaskJSON leaves no match and a valid document,
// and changes nothing but a space after such a colon.
func TestMaskJSON(t *testing.T) {
	for _, in := range []string{"C:<", "C:>", "C:&", "a:\n", "x:\u2028", "C:\xff", `C:"`, "\x1a:<", "C:<D:<", "fine: text", "no colon"} {
		body, err := json.Marshal(map[string]string{"reason": Mask(in)})
		if err != nil {
			t.Fatal(err)
		}
		out, ok := MaskJSON(body)
		if !ok {
			t.Errorf("MaskJSON(%q) = %s, not ok", body, out)
			continue
		}
		if p, found := Find(out); found {
			t.Errorf("MaskJSON(%q) = %s still holds %q", body, out, p)
		}
		var got map[string]string
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("MaskJSON(%q) = %s: %v", body, out, err)
		}
		want := strings.ToValidUTF8(in, "\uFFFD")
		if strings.ReplaceAll(got["reason"], ": ", ":") != strings.ReplaceAll(want, ": ", ":") {
			t.Errorf("MaskJSON changed %q into %q", want, got["reason"])
		}
	}
}
