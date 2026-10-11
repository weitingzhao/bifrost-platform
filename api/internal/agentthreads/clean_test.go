package agentthreads

import (
	"strings"
	"testing"
	"time"
)

// TestNoticesStripControls is the ratchet for the thread pushes: a silent
// thread and a lost host, with every agent-written field carrying one
// character of each removed class.
func TestNoticesStripControls(t *testing.T) {
	dirty := controlSample()
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := at.Add(11 * time.Minute)
	cfg := DefaultConfig()
	title, body := cfg.Message(Thread{
		Thread: dirty, Vendor: dirty, Host: dirty, Work: dirty, Title: dirty,
		Event: Event(dirty), Tool: dirty, ToolTimeoutS: 30,
		At: at, TurnStartedAt: at,
	}, now)
	hostTitle, hostBody := cfg.HostMessage(Host{
		Host: dirty, At: at,
		Vendors: map[string]VendorReport{},
	}, []Thread{{
		Title: dirty, Vendor: dirty, Thread: dirty, Host: dirty, Tool: dirty,
		Event: Event(dirty),
	}}, now)
	for _, tc := range []struct{ name, title, body string }{
		{"silent", title, body},
		{"host", hostTitle, hostBody},
	} {
		if r, ok := removedRune(tc.title, false); ok {
			t.Fatalf("%s title contains U+%04X (%q)", tc.name, r, tc.title)
		}
		if r, ok := removedRune(tc.body, true); ok {
			t.Fatalf("%s body contains U+%04X (%q)", tc.name, r, tc.body)
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
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) || r == 0x2028 || r == 0x2029 || removedMark(r) {
			return r, true
		}
	}
	return 0, false
}

func removedMark(r rune) bool {
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
