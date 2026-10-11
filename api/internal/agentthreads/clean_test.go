package agentthreads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

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
		if splitMarker(tc.title) || splitMarker(tc.body) {
			t.Fatalf("%s split a marker", tc.name)
		}
	}
}

func TestNoticesWithholdHiddenSecrets(t *testing.T) {
	rows := []string{
		"token=s3cr3\u200btvalue",
		"token=\u200bs3cr3tvalue",
		"api_key=abcd\aefgh12345678",
		"password=hunter2\u202ehunter2",
		"Bearer abcdefgh\u200bijklmnop",
	}
	parts := []string{"s3cr3", "tvalue", "efgh12345678", "hunter2", "abcdefgh", "ijklmnop"}
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := at.Add(11 * time.Minute)
	cfg := DefaultConfig()
	check := func(name, title, body string) {
		t.Helper()
		text := title + "\n" + body
		for _, p := range parts {
			if strings.Contains(text, p) {
				t.Fatalf("%s leaked %q\n%s", name, p, text)
			}
		}
		if r, ok := removedRune(title, false); ok {
			t.Fatalf("%s title U+%04X", name, r)
		}
		if r, ok := removedRune(body, true); ok {
			t.Fatalf("%s body U+%04X", name, r)
		}
		if splitMarker(text) {
			t.Fatalf("%s split a marker: %s", name, text)
		}
	}
	for _, row := range rows {
		base := Thread{
			Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
			Event: BeforeTool, Tool: "Bash", At: at, TurnStartedAt: at,
		}
		for _, field := range []string{"title", "thread", "vendor", "host", "work", "event", "tool"} {
			th := base
			switch field {
			case "title":
				th.Title = row
			case "thread":
				th.Thread = row
			case "vendor":
				th.Vendor = row
			case "host":
				th.Host = row
			case "work":
				th.Work = row
			case "event":
				th.Event = Event(row)
			case "tool":
				th.Tool = row
			}
			title, body := cfg.Message(th, now)
			check("silent/"+field, title, body)
		}
		title, body := cfg.HostMessage(Host{Host: row, At: at}, []Thread{{
			Title: row, Vendor: row, Thread: row, Tool: row, Event: Event(row),
		}}, now)
		check("host", title, body)
	}
}

func TestNoticesPassTheSendDoor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	at := time.Date(2026, 10, 11, 4, 4, 0, 0, time.UTC)
	now := at.Add(11 * time.Minute)
	cfg := DefaultConfig()
	title, body := cfg.Message(Thread{
		Thread: "7e939cd5abcd", Vendor: "claude", Host: "mbp", Work: "W-65",
		Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120,
		At: at, TurnStartedAt: at,
	}, now)
	hostTitle, hostBody := cfg.HostMessage(Host{Host: "mbp", At: at}, nil, now)
	for _, tc := range []struct{ title, body string }{{title, body}, {hostTitle, hostBody}} {
		ds, err := notice.Send(context.Background(), notice.Message{Title: tc.title, Message: tc.body})
		if err != nil || len(ds) != 1 || ds[0].Result != notice.ResultAccepted {
			t.Fatalf("door err=%v ds=%+v title %q", err, ds, tc.title)
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

func removedRune(s string, allowLF bool) (rune, bool) {
	for _, r := range s {
		if allowLF && r == '\n' {
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

func splitMarker(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		rest := s[i:]
		if !strings.HasPrefix(rest, "[redacted") && !strings.HasPrefix(rest, "[withheld") {
			continue
		}
		whole := false
		for _, m := range []string{
			"[withheld: hidden characters]",
			"[redacted private key]",
			"[redacted aws key]",
			"[redacted token]",
			"[redacted jwt]",
			"[redacted]",
		} {
			if strings.HasPrefix(rest, m) {
				whole = true
				i += len(m) - 1
				break
			}
		}
		if !whole {
			return true
		}
	}
	return false
}
