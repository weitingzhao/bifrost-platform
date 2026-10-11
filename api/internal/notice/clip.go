package notice

// Markers a clip must not cut through. Longer phrases are matched first so
// "[redacted]" is not treated as the whole of "[redacted private key]".
var clipMarkers = []string{
	WithheldMarker,
	"[redacted private key]",
	"[redacted aws key]",
	"[redacted token]",
	"[redacted jwt]",
	"[redacted]",
}

// ClipBytes keeps at most n bytes, with an ellipsis when it cuts, and does
// not cut a withheld or [redacted …] marker in half. A cut that would land
// inside a marker ends at the marker's start. A field that is only a marker
// is kept whole.
func ClipBytes(s string, n int) string {
	if n < 1 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := n - 1
	for cut > 0 && cut < len(s) && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	cut = clipCut(s, cut)
	if cut <= 0 {
		if m := leadingMarkerLen(s); m > 0 {
			return s[:m]
		}
		return "…"
	}
	return s[:cut] + "…"
}

// ClipRunes keeps at most n runes, with an ellipsis when it cuts, and does
// not cut a withheld or [redacted …] marker in half.
func ClipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	cut := len(string(r[:n-1]))
	cut = clipCut(s, cut)
	if cut <= 0 {
		if m := leadingMarkerLen(s); m > 0 {
			return s[:m]
		}
		return "…"
	}
	return s[:cut] + "…"
}

// ClipRunesHard keeps at most n runes and does not add an ellipsis.
// A cut inside a marker ends at the marker's start. A leading marker is kept whole.
func ClipRunesHard(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	cut := len(string(r[:n]))
	cut = clipCut(s, cut)
	if cut <= 0 {
		if m := leadingMarkerLen(s); m > 0 {
			return s[:m]
		}
		return ""
	}
	return s[:cut]
}

func clipCut(s string, cut int) int {
	best := cut
	i := 0
	for i < len(s) {
		n := markerLen(s[i:])
		if n == 0 {
			i++
			continue
		}
		end := i + n
		if cut > i && cut < end && i < best {
			best = i
		}
		i = end
	}
	return best
}

func markerLen(s string) int {
	for _, m := range clipMarkers {
		if stringsHasPrefix(s, m) {
			return len(m)
		}
	}
	return 0
}

func leadingMarkerLen(s string) int {
	return markerLen(s)
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
