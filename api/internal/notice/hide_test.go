package notice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHiddenRanges(t *testing.T) {
	for r := rune(0); r < 0x20; r++ {
		want := r != '\t' && r != '\n' && r != '\r'
		if HasHidden(string(r)) != want {
			t.Fatalf("U+%04X hidden=%v want %v", r, !want, want)
		}
	}
	if !HasHidden(string(rune(0x7F))) {
		t.Fatal("DEL")
	}
	for r := rune(0x80); r <= 0x9F; r++ {
		if !HasHidden(string(r)) {
			t.Fatalf("C1 U+%04X", r)
		}
	}
	ranges := []struct{ lo, hi rune }{
		{0x200B, 0x200D},
		{0x202A, 0x202E},
		{0x2061, 0x2064},
		{0x2066, 0x2069},
		{0xFE00, 0xFE0F},
		{0xE0000, 0xE007F},
	}
	for _, rg := range ranges {
		for r := rg.lo; r <= rg.hi; r++ {
			if !HasHidden(string(r)) {
				t.Fatalf("U+%04X", r)
			}
		}
	}
	for _, r := range []rune{0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x180E, 0x200E, 0x200F, 0x2028, 0x2029, 0x2060, 0xFEFF} {
		if !HasHidden(string(r)) {
			t.Fatalf("U+%04X", r)
		}
	}
	for _, r := range []rune{'\t', '\n', '\r', ' ', 'a', 'Ω', '\u017f', '\u212a', '\uFFFD', '\u2065', '\u00A0', '\u200A', '\uFE10', 0xE0080} {
		if HasHidden(string(r)) {
			t.Fatalf("U+%04X is not hidden", r)
		}
	}
	if HasHidden("ok\nline\tend\r") {
		t.Fatal("tab, LF and CR are posted")
	}
	if !HasHidden("\xff") || !HasHidden("a\xffb") || !HasHidden(string([]byte{0xC0})) {
		t.Fatal("invalid UTF-8")
	}
	if utf8.ValidString("\xff") {
		t.Fatal("fixture")
	}
}

func TestWithheldCopyAndSamplesAreTheClasses(t *testing.T) {
	if HasHidden(WithheldTitle) || HasHidden(WithheldBody) {
		t.Fatal("the stock fallback is hidden")
	}
	seen := map[string]bool{}
	for _, s := range HiddenSamples() {
		if seen[s.Name] {
			t.Fatalf("duplicate %s", s.Name)
		}
		seen[s.Name] = true
		if s.Name == "utf8" {
			if utf8.ValidString(s.Text) || !HasHidden(s.Text) {
				t.Fatalf("utf8 sample %q", s.Text)
			}
			continue
		}
		if !HasHidden(s.Text) || utf8.RuneCountInString(s.Text) != 1 {
			t.Fatalf("%s %q", s.Name, s.Text)
		}
	}
	for _, name := range []string{"c0", "del", "c1", "bidi-202e", "zw-200b", "ls", "ps", "shy", "utf8", "tag-start", "tag-end"} {
		if !seen[name] {
			t.Fatalf("missing class %s", name)
		}
	}
}

func TestPostedReplacesTitleAndBodyTogether(t *testing.T) {
	msg := Message{
		Title: "Decide · #7 · Sync \u202e app", Message: "Runs as soon as you approve.\nname=app",
		ClickURL: "http://ops.bifrost.lan/#approvals?id=appr_pin", Priority: 5,
		Fallback: "Decide · #7", Kind: "created", ID: "appr_pin",
	}
	got := Posted(msg)
	if got.Title != "Decide · #7" || got.Message != WithheldBody || got.ClickURL != msg.ClickURL || got.Priority != 5 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Title+got.Message, "Sync") || strings.Contains(got.Title+got.Message, "\u202e") || strings.Contains(got.Message, "name=") {
		t.Fatalf("composed text remained: %+v", got)
	}
	bodyOnly := msg
	bodyOnly.Title = "Decide · #7 · Sync app"
	bodyOnly.Message = "line \u200b"
	got = Posted(bodyOnly)
	if got.Title != "Decide · #7" || got.Message != WithheldBody {
		t.Fatalf("body: %+v", got)
	}
	clean := Message{Title: "Decide · #7 · Sync app", Message: "ok\nline", ClickURL: msg.ClickURL, Priority: 4, Fallback: "Decide · #7"}
	if got := Posted(clean); got.Title != clean.Title || got.Message != clean.Message || got.ClickURL != clean.ClickURL || got.Priority != 4 {
		t.Fatalf("clean %+v", got)
	}
}

func TestPostedWithoutACleanFallbackUsesTheStockTitle(t *testing.T) {
	msg := Message{Title: "Release \u202e policy", Message: "sign", ClickURL: "http://ops.bifrost.lan/#approvals", Priority: 4}
	got := Posted(msg)
	if got.Title != WithheldTitle || got.Message != WithheldBody || got.ClickURL != msg.ClickURL {
		t.Fatalf("%+v", got)
	}
	bad := msg
	bad.Fallback = "Decide \u202e #7"
	got = Posted(bad)
	if got.Title != WithheldTitle || strings.Contains(got.Title+got.Message, "\u202e") {
		t.Fatalf("bad fallback %+v", got)
	}
}

func TestSendPostsTheFallbackAndLogsOnce(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const mark = "Ω"
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	msg := Message{
		Title: "Look · " + mark + "\u202e", Message: "Nothing is working on it.",
		ClickURL: "http://ops.bifrost.lan/#needs-you", Priority: 3,
		Fallback: "Look · thread", Kind: "thread", ID: "claude/abc",
	}
	ds, err := Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Result != ResultAccepted || ds[0].Channel != "ntfy" || ds[0].Target != "phone" || ds[0].Kind != "" || ds[0].Error != "" {
		t.Fatalf("delivery %+v", ds)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["title"] != "Look · thread" || body["message"] != WithheldBody || body["click_url"] != msg.ClickURL || body["priority"] != float64(3) {
		t.Fatalf("posted %#v", body)
	}
	if _, ok := body["fallback"]; ok {
		t.Fatal("fallback was posted")
	}
	blob := string(raw)
	if strings.Contains(blob, mark) || strings.Contains(blob, "\u202e") || !utf8.ValidString(blob) {
		t.Fatalf("relay saw the field: %q", blob)
	}
	text := logs.String()
	if strings.Count(text, "notice withheld") != 1 || !strings.Contains(text, "kind=thread") || !strings.Contains(text, "id=claude/abc") {
		t.Fatalf("log %q", text)
	}
	if strings.Contains(text, mark) || strings.Contains(text, "\u202e") || strings.Contains(text, "Nothing is working") {
		t.Fatalf("log has the text: %q", text)
	}
}

func TestSendDoesNotLogAHiddenID(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"sent"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	_, err := Send(context.Background(), Message{Title: "x\u0007", Message: "y", Fallback: "Notification withheld", Kind: "host", ID: "mbp\u2028"})
	if err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	if strings.Contains(text, "mbp") || strings.Contains(text, "\u2028") || strings.Contains(text, "\u0007") {
		t.Fatalf("log %q", text)
	}
	if !strings.Contains(text, "kind=host") || !strings.Contains(text, "id=") {
		t.Fatalf("log %q", text)
	}
}

func TestCleanSamplesHaveNoHiddenCharacter(t *testing.T) {
	n := 64
	if !testing.Short() {
		n = 1000
	}
	got := CleanSamples(n)
	if len(got) != n {
		t.Fatalf("len %d", len(got))
	}
	for i, s := range got {
		if HasHidden(s) || s == "" {
			t.Fatalf("%d %q", i, s)
		}
	}
}
