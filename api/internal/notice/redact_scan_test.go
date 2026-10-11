package notice

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestKeywordAssignmentScanMatchesTheRegexp(t *testing.T) {
	legacy := regexp.MustCompile(`([` + foldAZ + `0-9_.-]*(?:` + secretWords + `)[` + foldAZ + `0-9_.-]*)(["']?` + asciiWS + `*[:=]` + asciiWS + `*["']?)` + notSecretValue + `+`)
	same := func(s string) {
		t.Helper()
		got := replaceKeyword(s, nil)
		want := replaceBounded(s, legacy, "$1$2[redacted]", nil)
		if got != want {
			t.Fatalf("in %q\n got %q\nwant %q", s, got, want)
		}
	}
	samples := []string{
		"token=s3cr3tvalue",
		"token='s3cr3tvalue'",
		"token=\"s3cr3tvalue\"",
		"token = s3cr3tvalue",
		"token = 's3cr3tvalue'",
		"token=' = 's3cr3tvalue",
		"pa\u017fsword=s3cr3tvalue",
		"token=password=secret",
		"xx=password=s3cr3tvalue",
		"note token=s3cr3tvalue end",
		"token:=s3cr3tvalue",
		"token:='s3cr3tvalue'",
		"'token'=s3cr3tvalue",
		"my_password=s3cr3tvalue",
		"password=",
		"==password=s3cr3tvalue",
		"aaaa",
		"\u017f\u017f\u017f",
		"token\t=\t's3cr3tvalue'",
		"pre password = s3cr3tvalue post",
		"token=s3cr3tvalue,password=othersecret",
		"token=s3cr3tvalue;password=othersecret",
		"not a secret",
		"api-key=s3cr3tvalue",
		"api_key=s3cr3tvalue",
		"apikey=s3cr3tvalue",
		"APIKEY=s3cr3tvalue",
		"to\u212aen=s3cr3tvalue",
		"x\u017fecret=abcdefgh12345678",
		"\u017fecret=abcdefgh12345678",
		"token= s3cr3tvalue",
		"token =\"s3cr3tvalue\"",
		"foo token='s3cr3tvalue' bar password=othersecret",
		"password password=s3cr3tvalue",
		"secret:s3cr3tvalue",
		strings.Repeat("a", 8000),
		strings.Repeat("\u017f", 8000),
		strings.Repeat("a", 4000) + "=x",
		"password" + strings.Repeat("a", 2000),
		strings.Repeat("a", 2000) + "password=s3cr3tvalue" + strings.Repeat("b", 2000),
	}
	for _, s := range samples {
		same(s)
	}
	rng := rand.New(rand.NewSource(307))
	alpha := []rune("abcABCXYZ013_=:'\" \t\n,;.-passwordtokenkey\u017f\u212a")
	for n := 0; n < 4000; n++ {
		buf := make([]rune, 1+rng.Intn(48))
		for i := range buf {
			buf[i] = alpha[rng.Intn(len(alpha))]
		}
		same(string(buf))
	}
}

func TestRedactScanIsLinear(t *testing.T) {
	inputs := []string{
		strings.Repeat("a", 8000),
		strings.Repeat("\u017f", 8000),
	}
	for _, n := range []int{1000, 2000, 4000} {
		inputs = append(inputs, strings.Repeat("Zxoxb-", n)+strings.Repeat("a", 20))
	}
	for _, s := range inputs {
		start := time.Now()
		n := redactScanSteps(s)
		elapsed := time.Since(start)
		runes := utf8.RuneCountInString(s)
		t.Logf("runes=%d bytes=%d steps=%d us=%d", runes, len(s), n, elapsed.Microseconds())
		// The floor is two walks, not one tick per failed candidate. The
		// "Zxoxb-" input used to report about one step per byte while the
		// regexp rescanned its tail on every candidate.
		if n < 2*runes || n > 16*runes {
			t.Fatalf("steps=%d runes=%d", n, runes)
		}
		if Redact(s) != redactWithRegexp(s) {
			t.Fatalf("linear scan diverged on %q", s[:16])
		}
	}
}

func TestLinearScanMatchesRegexp(t *testing.T) {
	same := func(s string) {
		t.Helper()
		got, want := Redact(s), redactWithRegexp(s)
		if got != want {
			t.Fatalf("in %q\n got %q\nwant %q", s, got, want)
		}
	}
	samples := []string{
		"bearer abcdefgh",
		"BeArEr abcdefgh",
		"xbearer abcdefgh",
		"x\tbearer abcdefgh",
		"Ba\u017fic abcdefgh",
		"basic\tabcdefgh",
		"bearer  abcdefgh bearer abcdefgh",
		"AKIAAAAAAAAAAAAAAAAA",
		"ZAKIAAAAAAAAAAAAAAAAA",
		"AKIAAAAAAAAAAAAAAAAAA",
		"ASIA" + strings.Repeat("B", 16),
		"eyJQQQQQQQQ.RRRRRRRR.SSSSSSSS",
		"xeyJQQQQQQQQ.RRRRRRRR.SSSSSSSS",
		"ghp_" + strings.Repeat("a", 20),
		"ghp_" + strings.Repeat("a", 20) + "_tail",
		"ghp_" + strings.Repeat("a", 19),
		"glpat-" + strings.Repeat("b", 20),
		"xoxb-" + strings.Repeat("c", 10),
		"Zxoxb-aaaaaaaaaa-xoxb-bbbbbbbbbb",
		strings.Repeat("Zxoxb-", 40) + strings.Repeat("a", 20),
		"token=s3cr3tvalue bearer abcdefgh",
		"postgres://app:QQQQQQQQ@db/x",
		"-----BEGIN PRIVATE KEY-----\nQQQQQQQQ\n-----END PRIVATE KEY-----",
		"",
		"aaaa",
		strings.Repeat("a", 200),
		strings.Repeat("\u017f", 40),
	}
	for _, s := range samples {
		same(s)
	}
	rng := rand.New(rand.NewSource(3071))
	alpha := []rune("abcABCXYZ013_=:'\" \t\n,;.-passwordtokenkeyZxoxb-AKIAeyJghp_glpat\u017f\u212a")
	for n := 0; n < 2000; n++ {
		buf := make([]rune, rng.Intn(80))
		for i := range buf {
			buf[i] = alpha[rng.Intn(len(alpha))]
		}
		same(string(buf))
	}
}

func redactWithRegexp(s string) string {
	for _, r := range redactions {
		switch r.scan {
		case scanKeyword:
			s = replaceKeyword(s, nil)
		case scanRegex:
			s = r.re.ReplaceAllString(s, r.repl)
		default:
			s = replaceBounded(s, r.re, r.repl, nil)
		}
	}
	return s
}
