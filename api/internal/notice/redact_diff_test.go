package notice

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestRedactHidesEverySecretMainHides is the differential against redactAt8b3259a.
// An input is prefix + SECRET + suffix, built from the keywords and schemes the
// patterns know. Letters s and k are expanded to s/S/U+017F and k/K/U+212A.
// If the oracle output does not contain SECRET, Redact must not contain it either.
func TestRedactHidesEverySecretMainHides(t *testing.T) {
	const longS = "\u017f"
	const kelvin = "\u212a"
	secrets := []string{
		"QQQQQQQQ",
		"QQQQQQQ" + kelvin,
		"QQQ" + longS + "QQQQ",
		"QQQQ.QQQQ",
	}
	keywords := []string{
		"password", "passwd", "pwd", "secret", "token",
		"apikey", "api_key", "api-key",
		"accesskey", "access_key", "access-key",
		"privatekey", "private_key", "private-key",
		"credential",
	}
	assignSeps := []string{
		"=", ":", " = ", "\t=\t", "\n:\n", "\r=\r", "\f=\f",
		"='", "=\"", "'='", "\"=\"", " = '", " = \"",
		",", " ", "\n", "\t", ";", "\"", "'", "",
	}
	ws := []string{" ", "\n", "\t", "\r", "\f", "  ", " \t"}
	prefixes := []string{"", " ", "\n", "\t", ",", "x", "note ", "\"", "'"}
	suffixes := []string{"", " ", "\n", "\t", ",", ".", " end", "\"", "'"}
	affixTail := []string{longS, longS + "uffix", kelvin, kelvin + "ind"}
	affixHead := []string{longS, "x" + longS, kelvin, "pre" + kelvin}

	var exhaustive, randomN, enforced int
	check := func(in, secret string) bool {
		old := redactAt8b3259a(in)
		got := Redact(in)
		if strings.Contains(old, secret) {
			return false
		}
		if strings.Contains(got, secret) {
			t.Fatalf("input %q\noracle %q\nredact %q", in, old, got)
		}
		return true
	}

	start := time.Now()
	for _, kw := range keywords {
		forms := foldForms(kw)
		for _, form := range forms {
			for _, sep := range assignSeps {
				for _, sec := range secrets {
					exhaustive++
					if check(form+sep+sec, sec) {
						enforced++
					}
				}
			}
		}
		for _, form := range forms[:1] {
			for _, tail := range affixTail {
				for _, sep := range assignSeps {
					for _, sec := range secrets {
						exhaustive++
						if check(form+tail+sep+sec, sec) {
							enforced++
						}
					}
				}
			}
			for _, head := range affixHead {
				for _, sep := range []string{"=", ":", " = ", "='"} {
					for _, sec := range secrets {
						exhaustive++
						if check(head+form+sep+sec, sec) {
							enforced++
						}
					}
				}
			}
		}
		for _, pre := range prefixes {
			for _, suf := range suffixes {
				exhaustive++
				if check(pre+kw+"="+secrets[0]+suf, secrets[0]) {
					enforced++
				}
			}
		}
	}
	for _, scheme := range []string{"bearer", "basic"} {
		for _, form := range foldForms(scheme) {
			for _, w := range ws {
				for _, sec := range secrets {
					for _, pre := range prefixes {
						for _, suf := range suffixes {
							exhaustive++
							if check(pre+form+w+sec+suf, sec) {
								enforced++
							}
						}
					}
				}
			}
		}
	}
	splits := []string{" ", "\n", "\t", ",", ";", ":"}
	for _, sp := range splits {
		for _, sec := range secrets {
			exhaustive += 4
			if check("bearer "+sec+sp+"bearer "+sec, sec) {
				enforced++
			}
			if check("basic "+sec+sp+"token="+sec, sec) {
				enforced++
			}
			if check("token="+sec+sp+"password="+sec, sec) {
				enforced++
			}
			if check(sec+sp+"bearer "+sec, sec) {
				enforced++
			}
		}
	}
	measured := []struct{ in, secret string }{
		{"Ba" + longS + "ic abcdefgh", "abcdefgh"},
		{"Bearer abcdefg" + kelvin, "abcdefg" + kelvin},
		{"token" + longS + "uffix=s3cr3tvalue", "s3cr3tvalue"},
		{longS + "ecret=abcdefgh12345678", "abcdefgh12345678"},
		{"x" + longS + "ecret=abcdefgh12345678", "abcdefgh12345678"},
		{"pa" + longS + longS + "word=s3cr3tvalue", "s3cr3tvalue"},
	}
	for _, m := range measured {
		exhaustive++
		if check(m.in, m.secret) {
			enforced++
		}
	}
	awsA := "AKIAAAAAAAAAAAAAAAAA"
	awsB := "ASIABBBBBBBBBBBBBBBB"
	gh := "ghp_" + strings.Repeat("a", 20)
	gl := "glpat-" + strings.Repeat("b", 20)
	xox := "xoxb-" + strings.Repeat("c", 10)
	jwt := "eyJQQQQQQQQ.RRRRRRRR.SSSSSSSS"
	pem := "-----BEGIN PRIVATE KEY-----\nQQQQQQQQ\n-----END PRIVATE KEY-----"
	for _, sp := range []string{"", " ", "\n", "\t", ",", "!", " start ", " end"} {
		exhaustive += 2
		if check(awsA+sp+awsB, awsA) {
			enforced++
		}
		if check(awsA+sp+awsB, awsB) {
			enforced++
		}
	}
	for _, in := range []string{gh, " " + gh + " ", gh + "_tail", gh + "\n" + gl + " " + xox, "x" + gh} {
		exhaustive++
		if check(in, gh) {
			enforced++
		}
	}
	exhaustive += 3
	if check(" "+jwt+" ", jwt) {
		enforced++
	}
	if check(pem, "QQQQQQQQ") {
		enforced++
	}
	if check("postgres://app:QQQQQQQ"+kelvin+"@db/x", "QQQQQQQ"+kelvin) {
		enforced++
	}
	exhaustiveDur := time.Since(start)

	rng := rand.New(rand.NewSource(65))
	noiseAlpha := []rune("abcABCxyzXYZ013456789 ._-:,;!'\n\t/" + longS + kelvin)
	noise := func(n int) string {
		buf := make([]rune, n)
		for i := range buf {
			buf[i] = noiseAlpha[rng.Intn(len(noiseAlpha))]
		}
		return string(buf)
	}
	schemeForms := append(foldForms("bearer"), foldForms("basic")...)
	var keywordForms []string
	for _, kw := range keywords {
		keywordForms = append(keywordForms, foldForms(kw)...)
	}
	validAssign := []string{"=", ":", " = ", "\t=\t", "\n:\n", "='", "=\"", "'='"}
	start = time.Now()
	var b strings.Builder
	for i := 0; i < 200_000; i++ {
		sec := secrets[rng.Intn(len(secrets))]
		b.Reset()
		chunks := 2 + rng.Intn(5)
		for c := 0; c < chunks; c++ {
			b.WriteString(noise(8 + rng.Intn(40)))
			b.WriteByte(' ')
			switch rng.Intn(3) {
			case 0:
				form := keywordForms[rng.Intn(len(keywordForms))]
				if rng.Intn(2) == 0 {
					form += affixTail[rng.Intn(len(affixTail))]
				}
				b.WriteString(form)
				b.WriteString(validAssign[rng.Intn(len(validAssign))])
				b.WriteString(sec)
			case 1:
				b.WriteString(schemeForms[rng.Intn(len(schemeForms))])
				b.WriteString(ws[rng.Intn(len(ws))])
				b.WriteString(sec)
			default:
				b.WriteString("token=")
				b.WriteString(sec)
				b.WriteString(splits[rng.Intn(len(splits))])
				b.WriteString("bearer ")
				b.WriteString(sec)
			}
		}
		b.WriteString(noise(8 + rng.Intn(24)))
		randomN++
		if check(b.String(), sec) {
			enforced++
		}
	}
	randomDur := time.Since(start)
	t.Logf("exhaustive=%d random=%d total=%d enforced=%d exhaustive_ms=%d random_ms=%d",
		exhaustive, randomN, exhaustive+randomN, enforced, exhaustiveDur.Milliseconds(), randomDur.Milliseconds())
	if randomN < 200_000 {
		t.Fatalf("random=%d", randomN)
	}
}

// foldForms replaces every s/S with s, S, or U+017F and every k/K with k, K, or
// U+212A, and adds an uppercase copy plus a copy whose first ASCII letter is upper.
func foldForms(word string) []string {
	rs := []rune(word)
	type slot struct {
		i    int
		alts []rune
	}
	var slots []slot
	for i, r := range rs {
		switch r {
		case 's', 'S':
			slots = append(slots, slot{i, []rune{'s', 'S', '\u017f'}})
		case 'k', 'K':
			slots = append(slots, slot{i, []rune{'k', 'K', '\u212a'}})
		}
	}
	var out []string
	add := func(s string) {
		for _, prev := range out {
			if prev == s {
				return
			}
		}
		out = append(out, s)
	}
	if len(slots) == 0 {
		add(word)
		add(strings.ToUpper(word))
		add(upperFirstASCII(word))
		return out
	}
	buf := append([]rune(nil), rs...)
	var rec func(int)
	rec = func(si int) {
		if si == len(slots) {
			s := string(buf)
			add(s)
			add(upperFirstASCII(s))
			return
		}
		for _, a := range slots[si].alts {
			buf[slots[si].i] = a
			rec(si + 1)
		}
	}
	rec(0)
	add(strings.ToUpper(word))
	return out
}

func upperFirstASCII(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		if r >= 'a' && r <= 'z' {
			rs[i] = r - 'a' + 'A'
			return string(rs)
		}
	}
	return s
}

// TestOracleCaseFoldIsLongSAndKelvin records that Go's (?i) adds no letter to
// [A-Za-z] besides U+017F and U+212A. A third fold would be a class this
// redactor does not yet hide.
func TestOracleCaseFoldIsLongSAndKelvin(t *testing.T) {
	letter := regexp.MustCompile(`(?i)[A-Za-z]`)
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if !utf8.ValidRune(r) || !letter.MatchString(string(r)) {
			continue
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '\u017f' || r == '\u212a' {
			continue
		}
		t.Fatalf("oracle folds U+%04X, which Redact does not treat as a letter", r)
	}
}
