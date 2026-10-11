package approvalnotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
		if splitMarker(m.Title) || splitMarker(m.Message) {
			t.Fatalf("%s split a marker\n%s\n%s", kind, m.Title, m.Message)
		}
	}
}

func TestComposeWithholdsHiddenSecrets(t *testing.T) {
	rows := []string{
		"token=s3cr3\u200btvalue",
		"token=\u200bs3cr3tvalue",
		"api_key=abcd\aefgh12345678",
		"password=hunter2\u202ehunter2",
		"Bearer abcdefgh\u200bijklmnop",
	}
	parts := []string{"s3cr3", "tvalue", "efgh12345678", "hunter2", "abcdefgh", "ijklmnop"}
	fields := []string{"action", "env", "summary", "reason", "error", "requester", "thread", "work", "runner", "param"}
	if len(kindVerb) == 0 {
		t.Fatal("no kinds")
	}
	for _, field := range fields {
		for _, row := range rows {
			for kind := range kindVerb {
				m := Compose(kind, itemWith(field, row), now)
				text := m.Title + "\n" + m.Message
				for _, p := range parts {
					if strings.Contains(text, p) {
						t.Fatalf("%s field %s leaked %q\n%s", kind, field, p, text)
					}
				}
				if r, ok := removedRune(m.Title, false); ok {
					t.Fatalf("%s %s title U+%04X", kind, field, r)
				}
				if r, ok := removedRune(m.Message, true); ok {
					t.Fatalf("%s %s body U+%04X", kind, field, r)
				}
				if splitMarker(text) {
					t.Fatalf("%s %s split a marker: %s", kind, field, text)
				}
			}
		}
	}
}

func TestComposePassesTheSendDoor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	for kind := range kindVerb {
		ds := Send(context.Background(), kind, gitopsSync(), now)
		if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Error != "" {
			t.Fatalf("%s door: %+v", kind, ds)
		}
		ds = Send(context.Background(), kind, pinItem(), now)
		if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Error != "" {
			t.Fatalf("%s pin door: %+v", kind, ds)
		}
	}
}

func TestClipEndsBeforeAMarker(t *testing.T) {
	it := gitopsSync()
	it.Reason = strings.Repeat("b", 91) + " token=s3cr3tvalue"
	m := Compose(KindCreated, it, now)
	lead := strings.TrimPrefix(m.Title, "Decide · #58 · ")
	if strings.Contains(lead, "s3cr3tvalue") || strings.Contains(lead, "[red") || !strings.HasSuffix(lead, "…") {
		t.Fatalf("lead %q", lead)
	}
	if got := clip("[withheld: hidden characters]", 10); got != "…" {
		t.Fatalf("withheld clip %q", got)
	}
	if got := clip("hello world", 5); got != "hell…" {
		t.Fatalf("plain clip %q", got)
	}
}

func itemWith(field, sample string) Item {
	it := Item{
		ID: "appr_row", Number: 7, Action: "gitops_sync_app", Tier: "C", Env: "prod",
		Summary: "sync the app", KeyParams: map[string]string{"name": "app-prod"},
		Requester: "cursor", Thread: "slice", WorkID: "W-65", Runner: "platform",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	switch field {
	case "action":
		it.Action = sample
		it.Summary, it.Reason = "", ""
	case "env":
		it.Env = sample
		it.Summary, it.Reason = "", ""
	case "summary":
		it.Summary = sample
	case "reason":
		it.Reason = sample
	case "error":
		it.Error = sample
	case "requester":
		it.Requester = sample
	case "thread":
		it.Thread = sample
	case "work":
		it.WorkID = sample
	case "runner":
		it.Runner = sample
	case "param":
		it.KeyParams = map[string]string{"name": sample}
	default:
		panic(field)
	}
	return it
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
