package coord

import "testing"

// Owner 2026-11-22: observe explicit reply evidence without exposing message bodies or inventing a link.
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
