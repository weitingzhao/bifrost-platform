package approvalnotify

import (
	"strings"
	"testing"
	"time"
)

// TestComposeStripsControls is the ratchet for TD-306: every kind, every
// agent-written field carrying one character of each removed class, and the
// composed title and body contain none of those code points. The body may
// keep LF and CR.
func TestComposeStripsControls(t *testing.T) {
	dirty := controlSample()
	it := Item{
		ID: "appr_controls", Number: 7, Action: dirty, Tier: "D", Env: dirty,
		Summary: dirty, KeyParams: map[string]string{"name": dirty, "command": dirty},
		Requester: dirty, Thread: dirty, WorkID: dirty, Runner: dirty,
		Reason: dirty, Error: dirty,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(2 * time.Hour),
	}
	if len(kindVerb) == 0 {
		t.Fatal("no kinds")
	}
	for kind := range kindVerb {
		m := Compose(kind, it, now)
		if m.Title == "" && m.Message == "" {
			t.Fatalf("%s produced no message", kind)
		}
		if r, ok := removedRune(m.Title, false); ok {
			t.Fatalf("%s title contains U+%04X (%q)", kind, r, m.Title)
		}
		if r, ok := removedRune(m.Message, true); ok {
			t.Fatalf("%s body contains U+%04X (%q)", kind, r, m.Message)
		}
	}
}

func controlSample() string {
	var b strings.Builder
	b.WriteString("Aa")
	for r := rune(0); r < 0x20; r++ {
		if r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	for r := rune(0x7F); r <= 0x9F; r++ {
		b.WriteRune(r)
	}
	for _, r := range []rune{
		0x061C, 0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x180E, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF,
		0x2028, 0x2029,
	} {
		b.WriteRune(r)
	}
	b.WriteString("Zz\n\r")
	return b.String()
}

func removedRune(s string, keepNewlines bool) (rune, bool) {
	for _, r := range s {
		if keepNewlines && (r == '\n' || r == '\r') {
			continue
		}
		if isRemoved(r) {
			return r, true
		}
	}
	return 0, false
}

func isRemoved(r rune) bool {
	if r < 0x20 || (r >= 0x7F && r <= 0x9F) || r == 0x2028 || r == 0x2029 {
		return true
	}
	switch r {
	case 0x061C, 0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x180E, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
		return true
	default:
		return false
	}
}
