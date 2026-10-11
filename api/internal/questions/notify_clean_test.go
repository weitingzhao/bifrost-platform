package questions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func TestQuestionNoticeStripsControls(t *testing.T) {
	dirty := controlSample()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	q := Question{
		Number: 3, Title: dirty, Urgency: dirty, Requester: dirty, RequesterThread: dirty,
		ExpiresAt: now.Add(2 * time.Hour),
	}
	title, body := noticeTitle(q), noticeBody(q, now)
	if r, ok := removedRune(title, false); ok {
		t.Fatalf("title U+%04X %q", r, title)
	}
	if r, ok := removedRune(body, true); ok {
		t.Fatalf("body U+%04X %q", r, body)
	}
	if splitMarker(title) || splitMarker(body) {
		t.Fatalf("split marker\n%s\n%s", title, body)
	}
	if !strings.Contains(title, "[withheld: hidden characters]") || !strings.Contains(body, "[withheld: hidden characters]") {
		t.Fatalf("marker missing\n%s\n%s", title, body)
	}
}

func TestQuestionNoticeWithholdsHiddenSecrets(t *testing.T) {
	rows := []string{
		"token=s3cr3\u200btvalue",
		"token=\u200bs3cr3tvalue",
		"api_key=abcd\aefgh12345678",
		"password=hunter2\u202ehunter2",
		"Bearer abcdefgh\u200bijklmnop",
	}
	parts := []string{"s3cr3", "tvalue", "efgh12345678", "hunter2", "abcdefgh", "ijklmnop"}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, row := range rows {
		for _, field := range []string{"title", "urgency", "requester", "thread"} {
			q := Question{Number: 4, Title: "Where is the quota?", Urgency: "today", Requester: "cursor", ExpiresAt: now.Add(time.Hour)}
			switch field {
			case "title":
				q.Title = row
			case "urgency":
				q.Urgency = row
			case "requester":
				q.Requester = row
			case "thread":
				q.RequesterThread = row
			}
			title, body := noticeTitle(q), noticeBody(q, now)
			text := title + "\n" + body
			for _, p := range parts {
				if strings.Contains(text, p) {
					t.Fatalf("%s leaked %q\n%s", field, p, text)
				}
			}
			if r, ok := removedRune(title, false); ok {
				t.Fatalf("%s title U+%04X", field, r)
			}
			if r, ok := removedRune(body, true); ok {
				t.Fatalf("%s body U+%04X", field, r)
			}
			if splitMarker(text) {
				t.Fatalf("%s split a marker: %s", field, text)
			}
		}
	}
}

func TestQuestionNoticePassesTheSendDoor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	q := Question{Number: 1, Title: "Where is the quota?", Urgency: "today", Requester: "cursor", ExpiresAt: now.Add(time.Hour)}
	title, body := noticeTitle(q), noticeBody(q, now)
	ds, err := notice.Send(context.Background(), notice.Message{Title: title, Message: body})
	if err != nil || len(ds) != 1 || ds[0].Result != notice.ResultAccepted {
		t.Fatalf("door err=%v ds=%+v title %q body %q", err, ds, title, body)
	}
}

func TestQuestionTitleClipEndsBeforeAMarker(t *testing.T) {
	title := noticeTitle(Question{Number: 1, Title: strings.Repeat("b", 90) + " token=s3cr3tvalue"})
	lead := strings.TrimPrefix(title, "Answer · Q1 · ")
	if strings.Contains(lead, "s3cr3tvalue") || strings.Contains(lead, "[red") {
		t.Fatalf("lead %q", lead)
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
