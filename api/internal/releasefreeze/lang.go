package releasefreeze

// MemberOfL reports whether b is a sentence of the language L in design §3.2.
// The scan follows the productions. It does not call a JSON parser.
func MemberOfL(b []byte) bool {
	i := skipW(b, 0)
	i, ok := parseTop(b, i)
	if !ok {
		return false
	}
	i = skipW(b, i)
	return atEnd(len(b), i)
}

func atEnd(n, i int) bool { return i == n }

func isW(c byte) bool {
	return c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D
}

func skipW(b []byte, i int) int {
	for i < len(b) && isW(b[i]) {
		i++
	}
	return i
}

const (
	minDigits = 1
	maxDigits = 32
)

func membersOK(seen [4]bool, dups int) bool {
	if dups > 0 || seen != [4]bool{true, true, true, true} {
		return false
	}
	return true
}

func parseTop(b []byte, i int) (int, bool) {
	if i >= len(b) || b[i] != '{' {
		return i, false
	}
	i++
	var seen [4]bool
	dups := 0
	for n := 0; n < 4; n++ {
		var kind int
		var ok bool
		i, kind, ok = parseM(b, i)
		if !ok || kind < 0 || kind > 3 {
			return i, false
		}
		if seen[kind] {
			dups++
		}
		seen[kind] = true
		i = skipW(b, i)
		if n < 3 {
			if i >= len(b) || b[i] != ',' {
				return i, false
			}
			i++
		}
	}
	if !membersOK(seen, dups) {
		return i, false
	}
	if i >= len(b) || b[i] != '}' { // top ends after four members
		return i, false
	}
	return i + 1, true
}

func parseM(b []byte, i int) (int, int, bool) {
	i = skipW(b, i)
	key, i, ok := parseQuoted(b, i)
	if !ok {
		return i, 0, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != ':' {
		return i, 0, false
	}
	i++
	i = skipW(b, i)
	switch key {
	case "apiVersion":
		i, ok = expectQuoted(b, i, "release.bifrost.io/v1")
		return i, 0, ok
	case "kind":
		i, ok = expectQuoted(b, i, "FreezeList")
		return i, 1, ok
	case "metadata":
		i, ok = parseMeta(b, i)
		return i, 2, ok
	case "items":
		i, ok = parseItems(b, i)
		return i, 3, ok
	default:
		return i, 0, false
	}
}

func parseItems(b []byte, i int) (int, bool) {
	if i >= len(b) || b[i] != '[' {
		return i, false
	}
	i++
	i = skipW(b, i)
	if i >= len(b) || b[i] != ']' {
		return i, false
	}
	return i + 1, true
}

func parseMeta(b []byte, i int) (int, bool) {
	if i >= len(b) || b[i] != '{' {
		return i, false
	}
	start := i + 1
	if j, ok := metaR(b, start); ok {
		return j, true
	}
	if j, ok := metaCR(b, start); ok {
		return j, true
	}
	if j, ok := metaRC(b, start); ok {
		return j, true
	}
	return i, false
}

func metaR(b []byte, i int) (int, bool) {
	i = skipW(b, i)
	i, ok := parseR(b, i)
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != '}' {
		return i, false
	}
	return i + 1, true
}

func metaCR(b []byte, i int) (int, bool) {
	i = skipW(b, i)
	i, ok := parseC(b, i)
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != ',' {
		return i, false
	}
	i++
	i = skipW(b, i)
	i, ok = parseR(b, i)
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != '}' {
		return i, false
	}
	return i + 1, true
}

func metaRC(b []byte, i int) (int, bool) {
	i = skipW(b, i)
	i, ok := parseR(b, i)
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != ',' {
		return i, false
	}
	i++
	i = skipW(b, i)
	i, ok = parseC(b, i)
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != '}' {
		return i, false
	}
	return i + 1, true
}

func parseR(b []byte, i int) (int, bool) {
	i, ok := expectQuoted(b, i, "resourceVersion")
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != ':' {
		return i, false
	}
	i++
	i = skipW(b, i)
	if i >= len(b) || b[i] != '"' {
		return i, false
	}
	i++
	start := i
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	n := i - start
	if n < minDigits || n > maxDigits {
		return i, false
	}
	if i >= len(b) || b[i] != '"' {
		return i, false
	}
	return i + 1, true
}

func continueEmpty(s string) bool { return s == "" }

func parseC(b []byte, i int) (int, bool) {
	i, ok := expectQuoted(b, i, "continue")
	if !ok {
		return i, false
	}
	i = skipW(b, i)
	if i >= len(b) || b[i] != ':' {
		return i, false
	}
	i++
	i = skipW(b, i)
	s, i, ok := parseQuoted(b, i)
	if !ok || !continueEmpty(s) {
		return i, false
	}
	return i, true
}

func expectQuoted(b []byte, i int, want string) (int, bool) {
	got, j, ok := parseQuoted(b, i)
	if !ok || got != want {
		return i, false
	}
	return j, true
}

func parseQuoted(b []byte, i int) (string, int, bool) {
	if i >= len(b) || b[i] != '"' {
		return "", i, false
	}
	i++
	start := i
	for i < len(b) && b[i] != '"' {
		if b[i] == '\\' || b[i] < 0x20 {
			return "", i, false
		}
		i++
	}
	if i >= len(b) {
		return "", i, false
	}
	return string(b[start:i]), i + 1, true
}
