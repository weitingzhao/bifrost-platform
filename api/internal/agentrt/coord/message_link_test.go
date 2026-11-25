package coord

import (
	"strings"
	"testing"
)

// Observe explicit reply evidence without exposing message bodies or inventing a link (ISSUE-61).
func TestReplyReference(t *testing.T) {
	id := "m_01M82M4JYZ0CBW1S3SHS68KTM5"
	good := `{"v":1,"type":"note","summary":"reply","reply_to":"` + id + `","text":"private body"}`
	if got := replyReference([]byte(good)); got != id {
		t.Fatalf("got %q", got)
	}
	for _, body := range []string{`plain answer`, `{"v":2,"type":"note","summary":"reply","reply_to":"` + id + `"}`, `{"v":1,"type":"note","summary":"reply","reply_to":"not-an-id"}`, `{"reply_to":"` + id + `"}`} {
		if got := replyReference([]byte(body)); got != "" {
			t.Fatalf("unexpected link %q", got)
		}
	}
}

// 2026-11-24: cockpit asked for a letter's subject on message_posted. Only the
// sender's declared summary goes out, one line, cut to 140; a plain letter or
// an envelope's text gives nothing.
func TestLetterSummary(t *testing.T) {
	if got := letterSummary([]byte(`{"v":1,"type":"note","summary":"window\n fixed","text":"private body"}`)); got != "window fixed" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("长", 200)
	got := []rune(letterSummary([]byte(`{"v":1,"type":"ask","summary":"` + long + `"}`)))
	if len(got) != MaxLetterSummary || got[len(got)-1] != '…' {
		t.Fatalf("got %d runes", len(got))
	}
	for _, body := range []string{`plain answer`, `{"v":2,"type":"note","summary":"s"}`, `{"v":1,"summary":"s"}`, `{"v":1,"type":"note","summary":"  ","text":"private"}`} {
		if got := letterSummary([]byte(body)); got != "" {
			t.Fatalf("%s: unexpected summary %q", body, got)
		}
	}
}
