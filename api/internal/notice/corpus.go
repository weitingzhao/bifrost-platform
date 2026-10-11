package notice

import (
	"math/rand"
	"strings"
)

// CleanSamples returns n strings from the redactor differential's seeded
// random corpus (source 65). A string that HasHidden rejects is skipped, so
// every result is text Send posts unchanged. The construction matches the
// random half of TestRedactHidesEverySecretMainHides: the same keywords,
// schemes, separators and noise alphabet.
func CleanSamples(n int) []string {
	if n <= 0 {
		return nil
	}
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
	ws := []string{" ", "\n", "\t", "\r", "\f", "  ", " \t"}
	affixTail := []string{longS, longS + "uffix", kelvin, kelvin + "ind"}
	splits := []string{" ", "\n", "\t", ",", ";", ":"}
	noiseAlpha := []rune("abcABCxyzXYZ013456789 ._-:,;!'\n\t/" + longS + kelvin)
	validAssign := []string{"=", ":", " = ", "\t=\t", "\n:\n", "='", "=\"", "'='"}
	schemeForms := append(foldSample("bearer"), foldSample("basic")...)
	var keywordForms []string
	for _, kw := range keywords {
		keywordForms = append(keywordForms, foldSample(kw)...)
	}
	rng := rand.New(rand.NewSource(65))
	noise := func(n int) string {
		buf := make([]rune, n)
		for i := range buf {
			buf[i] = noiseAlpha[rng.Intn(len(noiseAlpha))]
		}
		return string(buf)
	}
	out := make([]string, 0, n)
	var b strings.Builder
	// The differential draws 200_000 of these. Form feed is hidden, so those
	// draws are skipped; the rest are the clean corpus, in the same order.
	for i := 0; i < 200_000 && len(out) < n; i++ {
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
		s := b.String()
		if HasHidden(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// foldSample is the differential test's foldForms, kept here so the seeded
// corpus does not depend on a test file.
func foldSample(word string) []string {
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
	upperFirst := func(s string) string {
		rs := []rune(s)
		for i, r := range rs {
			if r >= 'a' && r <= 'z' {
				rs[i] = r - 'a' + 'A'
				return string(rs)
			}
		}
		return s
	}
	if len(slots) == 0 {
		add(word)
		add(strings.ToUpper(word))
		add(upperFirst(word))
		return out
	}
	buf := append([]rune(nil), rs...)
	var rec func(int)
	rec = func(si int) {
		if si == len(slots) {
			s := string(buf)
			add(s)
			add(upperFirst(s))
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
