package notice

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The five inputs from the round-3 table, plus the bearer row that
// "strip, then redact" still split. Each has a removed-class character
// inside the secret, so the whole field is the marker.
func TestPushFieldTableRows(t *testing.T) {
	for _, in := range []string{
		"token=s3cr3\u200btvalue",
		"token=\u200bs3cr3tvalue",
		"api_key=abcd\aefgh12345678",
		"password=hunter2\u202ehunter2",
		"Bearer abcdefgh\u200bijklmnop",
	} {
		for _, title := range []bool{false, true} {
			if got := PushField(in, title); got != withheldMarker {
				t.Fatalf("PushField(%q, title=%v) = %q", in, title, got)
			}
		}
	}
}

// ASCII whitespace is normalised, then Redact. da7b774 hides U+017F and
// U+212A forms that redactAt8b3259a keeps, so the equality is against
// Redact of the normalised field, not against main.
func TestPushFieldUsesTheLiveRedactor(t *testing.T) {
	fold := "\u017fecret=s3cr3tvalue"
	if PushField(fold, false) != Redact(fold) {
		t.Fatalf("PushField(%q) = %q, Redact = %q", fold, PushField(fold, false), Redact(fold))
	}
	if !strings.Contains(redactAt8b3259a(fold), "s3cr3tvalue") || strings.Contains(Redact(fold), "s3cr3tvalue") {
		t.Fatalf("fixture no longer distinguishes da7b774 from main\n main %q\n live %q", redactAt8b3259a(fold), Redact(fold))
	}
	for _, title := range []bool{false, true} {
		for _, in := range []string{
			"password=abc\tdef",
			"password=\tabcdefgh",
			"token=s3cr3tvalue\r\nmore",
			"Bearer\tabcdefghijkl",
			"hello\fworld",
			"a\r\nb\rc\fd",
		} {
			got := PushField(in, title)
			want := Redact(normPushSpec(in, title))
			if got != want {
				t.Fatalf("title=%v %q\n got %q\nwant %q", title, in, got, want)
			}
			if got == withheldMarker {
				t.Fatalf("ASCII whitespace was withheld: %q", in)
			}
		}
	}
}

func TestPushFieldMatchesLiveRedactor(t *testing.T) {
	const n = 200_000
	ws := []string{" ", "\t", "\n", "\r", "\f", "\r\n", " \t", "\t\t", "\f\n", "\r\n\t"}
	secrets := []string{"s3cr3tvalue", "abcdefghijklmnop", "QQQQQQQQ", "hunter2hunter2"}
	keywords := []string{"token", "password", "api_key", "secret", "\u017fecret", "to\u212aen"}
	schemes := []string{"Bearer", "Basic", "bearer", "basic"}
	alpha := []rune("abcABCxyzXYZ013456789 ._-:,;!'/\t\n\r\f\u017f\u212a")
	rng := rand.New(rand.NewSource(306))
	noise := func(k int) string {
		buf := make([]rune, k)
		for i := range buf {
			buf[i] = alpha[rng.Intn(len(alpha))]
		}
		return string(buf)
	}
	var tabs, crs, lfs, ffs, folds int
	for i := 0; i < n; i++ {
		title := i%2 == 0
		var b strings.Builder
		switch i % 5 {
		case 0:
			b.WriteString(noise(4 + rng.Intn(12)))
			kw := keywords[rng.Intn(len(keywords))]
			b.WriteString(kw)
			b.WriteString(ws[rng.Intn(len(ws))])
			b.WriteByte('=')
			b.WriteString(ws[rng.Intn(len(ws))])
			b.WriteString(secrets[rng.Intn(len(secrets))])
			b.WriteString(noise(3))
			if strings.ContainsRune(kw, '\u017f') || strings.ContainsRune(kw, '\u212a') {
				folds++
			}
		case 1:
			b.WriteString(noise(2))
			b.WriteString(schemes[rng.Intn(len(schemes))])
			b.WriteString(ws[rng.Intn(len(ws))])
			b.WriteString(secrets[rng.Intn(len(secrets))])
			b.WriteString(noise(2))
		case 2:
			b.WriteString("AKIA")
			b.WriteString(strings.Repeat("A", 16))
			b.WriteString(ws[rng.Intn(len(ws))])
			b.WriteString(noise(4))
		case 3:
			b.WriteString(noise(8 + rng.Intn(24)))
		default:
			b.WriteString(noise(3))
			b.WriteString("\u017fecret=")
			b.WriteString(secrets[0])
			b.WriteString(ws[rng.Intn(len(ws))])
			folds++
		}
		in := b.String()
		if strings.Contains(in, "\t") {
			tabs++
		}
		if strings.Contains(in, "\r") {
			crs++
		}
		if strings.Contains(in, "\n") {
			lfs++
		}
		if strings.Contains(in, "\f") {
			ffs++
		}
		got := PushField(in, title)
		want := Redact(normPushSpec(in, title))
		if got != want {
			t.Fatalf("i=%d title=%v\nin %q\ngot %q\nwant %q", i, title, in, got, want)
		}
	}
	if tabs < 1000 || crs < 1000 || lfs < 1000 || ffs < 1000 || folds < 1000 {
		t.Fatalf("coverage tab=%d cr=%d lf=%d ff=%d fold=%d", tabs, crs, lfs, ffs, folds)
	}
	t.Logf("equality=Redact(normalised) clean=%d tab=%d cr=%d lf=%d ff=%d fold=%d", n, tabs, crs, lfs, ffs, folds)
}

func TestPushFieldWithholdsHiddenCharacters(t *testing.T) {
	const n = 200_000
	hidden := []rune{
		0x00, 0x01, 0x07, 0x08, 0x0B, 0x0E, 0x1B, 0x1F,
		0x7F, 0x80, 0x85, 0x9F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x061C, 0x200E, 0x200F,
		0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF,
		0x2028, 0x2029,
	}
	bases := []string{
		"token=s3cr3tvalue",
		"api_key=abcdefgh12345678",
		"password=hunter2hunter2",
		"Bearer abcdefghijklmnop",
		"Basic abcdefghijklmnop",
		"hello",
	}
	var alone, first, last, mid, keyword int
	for i := 0; i < n; i++ {
		h := string(hidden[i%len(hidden)])
		title := i%2 == 0
		base := bases[i%len(bases)]
		var in string
		switch i % 5 {
		case 0:
			in = h
			alone++
		case 1:
			in = h + base
			first++
		case 2:
			in = base + h
			last++
		case 3:
			rs := []rune(base)
			at := len(rs) / 2
			in = string(rs[:at]) + h + string(rs[at:])
			mid++
		default:
			in = base[:1] + h + base[1:]
			keyword++
		}
		if got := PushField(in, title); got != withheldMarker {
			t.Fatalf("i=%d title=%v %q → %q", i, title, in, got)
		}
	}
	t.Logf("dirty=%d alone=%d first=%d last=%d mid=%d inside=%d marker=%q", n, alone, first, last, mid, keyword, withheldMarker)
}

func TestSendRefusesUncleanTextAndSendsANormalOne(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	ds, err := Send(context.Background(), Message{Title: "Decide \u202e now", Message: "ok"})
	if err == nil || err.Error() != "unclean text" || len(ds) != 1 || ds[0].Result != ResultFailed || ds[0].Error != "unclean text" {
		t.Fatalf("unset relay: err=%v ds=%+v", err, ds)
	}

	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		_, _ = w.Write([]byte(`{"status":"sent","deliveries":[{"channel":"ntfy","target":"phone","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")

	for _, msg := range []Message{
		{Title: "Decide \u202e", Message: "body"},
		{Title: "Decide", Message: "has \u202e in the body"},
		{Title: "two\nlines", Message: "body"},
		{Title: "tab\there", Message: "body"},
		{Title: "Decide", Message: "tab\there"},
		{Title: "Decide", Message: "cr\rhere"},
	} {
		called = 0
		ds, err = Send(context.Background(), msg)
		if err == nil || err.Error() != "unclean text" || called != 0 || len(ds) != 1 || ds[0].Error != "unclean text" {
			t.Fatalf("%q / %q: err=%v called=%d ds=%+v", msg.Title, msg.Message, err, called, ds)
		}
	}

	ds, err = Send(context.Background(), Message{Title: "Decide · #1", Message: "Runs as soon as you approve.\nname=app"})
	if err != nil || called != 1 || len(ds) != 1 || ds[0].Result != ResultAccepted {
		t.Fatalf("normal: err=%v called=%d ds=%+v", err, called, ds)
	}
}

// normPushSpec is the normalisation the task states, written out here so a
// change inside PushField cannot satisfy the equality by sharing a helper.
func normPushSpec(s string, title bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\f", " ")
	if title {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	return s
}
