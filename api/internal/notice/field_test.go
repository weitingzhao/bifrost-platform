package notice

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFieldRedactsBeforeNormalisingPEM(t *testing.T) {
	raw := "-----BEGIN PRIVATE KEY-----X-----END\nPRIVATE KEY-----QQQQQQQQ"
	got := Field(raw, false)
	if strings.Contains(got, "QQQQQQQQ") || got != "[redacted private key]" {
		t.Fatalf("Field = %q", got)
	}
	// A tab is the whitespace the bearer rule needs. It is normalised after.
	tabbed := Field("x\tbearer abcdefgh", false)
	if strings.Contains(tabbed, "abcdefgh") || tabbed != "x bearer [redacted]" {
		t.Fatalf("tab bearer = %q", tabbed)
	}
}

func TestCleanFieldsMatchOracleThenNormalise(t *testing.T) {
	samples := cleanSamples(200_000)
	second := 0
	var examples []string
	for _, s := range samples {
		if HasHidden(s.raw) {
			t.Fatalf("clean corpus contains a hidden character: %q", s.raw)
		}
		oracle := redactAt8b3259a(s.raw)
		red := Redact(s.raw)
		for _, title := range []bool{false, true} {
			got := Field(s.raw, title)
			if red == oracle {
				if got != normalise(oracle, title) {
					t.Fatalf("title=%v raw %q\n got %q\nwant %q", title, s.raw, got, normalise(oracle, title))
				}
				continue
			}
			if title {
				second++
			}
			if got != normalise(red, title) {
				t.Fatalf("second branch title=%v raw %q\n got %q\nwant %q", title, s.raw, got, normalise(red, title))
			}
			for _, sec := range s.secrets {
				if sec == "" || !strings.Contains(s.raw, sec) || strings.Contains(oracle, sec) {
					continue
				}
				if strings.Contains(got, sec) {
					t.Fatalf("oracle hid %q but Field kept it in %q (raw %q)", sec, got, s.raw)
				}
			}
			if len(examples) < 6 && !title {
				examples = append(examples, s.raw)
			}
		}
	}
	if second == 0 {
		t.Fatal("no input took the Redact-differs-from-oracle branch")
	}
	t.Logf("clean=%d second_branch_inputs=%d examples=%q", len(samples), second, examples)
}

func TestHiddenFieldsAreTheMarker(t *testing.T) {
	raws := dirtySamples(200_000)
	seen := map[rune]bool{}
	for _, raw := range raws {
		if !HasHidden(raw) {
			t.Fatalf("dirty corpus was not hidden: %q", raw)
		}
		if utf8.ValidString(raw) {
			for _, r := range raw {
				if hiddenRune(r) {
					seen[r] = true
				}
			}
		}
		for _, title := range []bool{false, true} {
			if got := Field(raw, title); got != WithheldMarker {
				t.Fatalf("Field(%q, %v) = %q", raw, title, got)
			}
		}
	}
	for _, r := range hiddenCodePoints() {
		if !seen[r] {
			t.Fatalf("code point U+%04X never appeared", r)
		}
	}
	t.Logf("dirty=%d code_points=%d", len(raws), len(seen))
}

func TestSendDoorRefusesHiddenAndTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unclean text was posted")
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "tok")
	for _, tc := range []struct {
		title, body string
	}{
		{"Decide\nmore", "body"},
		{"Decide\there", "body"},
		{"Decide", "a\tb"},
		{"Decide \u202e", "body"},
		{"Decide", "a\u200bb"},
		{"Decide", string([]byte{0xff})},
	} {
		ds, err := Send(t.Context(), Message{Title: tc.title, Message: tc.body})
		if err == nil || err.Error() != "unclean text" || len(ds) != 1 || ds[0].Result != ResultFailed || ds[0].Error != "unclean text" {
			t.Fatalf("title %q body %q err %v deliveries %+v", tc.title, tc.body, err, ds)
		}
	}
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	for _, tc := range []Message{
		{Title: "Decide · #1 · patch", Message: "Runs as soon as you approve.\nfrom cursor"},
		{Title: "Decide", Message: "line\nline"},
	} {
		ds, err := Send(t.Context(), tc)
		if err != nil || len(ds) != 1 || ds[0].Result != ResultSkipped {
			t.Fatalf("%+v err %v deliveries %+v", tc, err, ds)
		}
	}
}

func TestClipDoesNotSplitMarkers(t *testing.T) {
	for _, m := range []string{WithheldMarker, "[redacted private key]", "[redacted]", "[redacted token]", "[redacted jwt]", "[redacted aws key]"} {
		if got := ClipBytes(m, 8); got != m {
			t.Fatalf("ClipBytes %q = %q", m, got)
		}
		if got := ClipRunes(m, 4); got != m {
			t.Fatalf("ClipRunes %q = %q", m, got)
		}
		if got := ClipRunesHard(m, 4); got != m {
			t.Fatalf("ClipRunesHard %q = %q", m, got)
		}
	}
	if got := ClipRunesHard("7e939cd5abcd", 8); got != "7e939cd5" {
		t.Fatalf("id cut = %q", got)
	}
	if got := ClipBytes("abcdefghij", 5); got != "abcd…" {
		t.Fatalf("byte clip = %q", got)
	}
}

func TestHiddenJSONCatchesUnpairedSurrogates(t *testing.T) {
	holds := HiddenJSONFields([]byte(`{"reason":"to\uD800ken","params":{"name":"ok\uD800\uDC00"}}`))
	if !holds["reason"] {
		t.Fatalf("unpaired surrogate was not hidden: %#v", holds)
	}
	if holds["params.name"] {
		t.Fatalf("a surrogate pair was treated as hidden: %#v", holds)
	}
	zw := HiddenJSONFields([]byte("{\"reason\":\"a\u200bb\"}"))
	if !zw["reason"] {
		t.Fatalf("literal hidden character: %#v", zw)
	}
	if got := HiddenJSONFields([]byte{0xff}); !got["*"] {
		t.Fatalf("invalid document: %#v", got)
	}
}

type cleanSample struct {
	raw     string
	secrets []string
}

func cleanSamples(n int) []cleanSample {
	var out []cleanSample
	add := func(raw string, secrets ...string) {
		out = append(out, cleanSample{raw, secrets})
	}
	pem := "-----BEGIN PRIVATE KEY-----X-----END\nPRIVATE KEY-----QQQQQQQQ"
	ws := []string{" ", "\t", "\n", "\r", "\f", "\r\n"}
	bases := []struct{ raw, secret string }{
		{"token=QQQQQQQQ", "QQQQQQQQ"},
		{"password=QQQQQQQQ", "QQQQQQQQ"},
		{"secret=QQQQQQQQ", "QQQQQQQQ"},
		{"api_key=QQQQQQQQ", "QQQQQQQQ"},
		{"Bearer abcdefgh", "abcdefgh"},
		{"basic abcdefgh", "abcdefgh"},
		{pem, "QQQQQQQQ"},
		{"https://user:QQQQQQQQ@example.com/x", "QQQQQQQQ"},
		{"eyJabcdefghij.abcdefghij.abcdefghij", "eyJabcdefghij.abcdefghij.abcdefghij"},
		{"AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},
		{"ghp_" + strings.Repeat("a", 20), "ghp_" + strings.Repeat("a", 20)},
		{"plain text about a node", ""},
		{"x\tbearer abcdefgh", "abcdefgh"},
	}
	for _, b := range bases {
		secs := []string{}
		if b.secret != "" {
			secs = []string{b.secret}
		}
		add(b.raw, secs...)
		for _, w := range ws {
			add(w+b.raw, secs...)
			add(b.raw+w, secs...)
			add(w+b.raw+w, secs...)
			if b.raw != "" {
				mid := len(b.raw) / 2
				add(b.raw[:mid]+w+b.raw[mid:], secs...)
			}
		}
	}
	for _, word := range []string{"token", "password", "secret", "bearer", "basic"} {
		for _, form := range foldForms(word) {
			switch word {
			case "bearer", "basic":
				add(form+" abcdefgh", "abcdefgh")
				add(form+"\tabcdefgh", "abcdefgh")
			default:
				add(form+"=QQQQQQQQ", "QQQQQQQQ")
				add(form+"\t=\tQQQQQQQQ", "QQQQQQQQ")
			}
		}
	}
	rng := rand.New(rand.NewSource(306))
	for len(out) < n {
		b := bases[rng.Intn(len(bases))]
		w := ws[rng.Intn(len(ws))]
		raw := fmt.Sprintf("note %d ", rng.Intn(1_000_000)) + w + b.raw + w + fmt.Sprintf(" tail %d", rng.Intn(1_000_000))
		secs := []string{}
		if b.secret != "" {
			secs = []string{b.secret}
		}
		add(raw, secs...)
	}
	return out[:n]
}

func dirtySamples(n int) []string {
	points := hiddenCodePoints()
	var out []string
	for _, r := range points {
		s := string(r)
		out = append(out,
			s,
			s+"head",
			"tail"+s,
			"to"+s+"ken=QQQQQQQQ",
			"token=QQ"+s+"QQQQQQ",
			"Bearer abcdefgh"+s+"ijkl",
		)
	}
	out = append(out,
		string([]byte{0xff}),
		string([]byte{0x80}),
		string([]byte{0xc3, 0x28}),
		"token=QQQQQQQQ"+string([]byte{0xff}),
	)
	rng := rand.New(rand.NewSource(307))
	for len(out) < n {
		r := points[rng.Intn(len(points))]
		out = append(out, fmt.Sprintf("pre%d", rng.Intn(1<<20))+string(r)+fmt.Sprintf("post%d", rng.Intn(1<<20)))
	}
	return out[:n]
}

func hiddenCodePoints() []rune {
	var out []rune
	for r := rune(0); r < 0x20; r++ {
		if r == '\t' || r == '\n' || r == '\f' || r == '\r' {
			continue
		}
		out = append(out, r)
	}
	out = append(out, 0x7F)
	for r := rune(0x80); r <= 0x9F; r++ {
		out = append(out, r)
	}
	out = append(out,
		0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x180E,
		0x200B, 0x200C, 0x200D, 0x200E, 0x200F,
		0x2028, 0x2029,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2060, 0x2061, 0x2062, 0x2063, 0x2064,
		0x2066, 0x2067, 0x2068, 0x2069,
		0xFEFF,
	)
	for r := rune(0xFE00); r <= 0xFE0F; r++ {
		out = append(out, r)
	}
	for r := rune(0xE0000); r <= 0xE007F; r++ {
		out = append(out, r)
	}
	return out
}
