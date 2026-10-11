package notice

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
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
	for _, s := range []string{strings.Repeat("a", 8000), strings.Repeat("\u017f", 8000)} {
		n := redactScanSteps(s)
		runes := utf8.RuneCountInString(s)
		t.Logf("runes=%d steps=%d", runes, n)
		if n < runes || n > 8*runes {
			t.Fatalf("steps=%d runes=%d", n, runes)
		}
		if Redact(s) != s {
			t.Fatalf("redact changed %q", s[:16])
		}
	}
}
