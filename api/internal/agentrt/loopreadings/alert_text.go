package loopreadings

import (
	"fmt"
	"unicode/utf8"
)

// FailureText formats the observed error; caller retains the bound, alert policy and writes.
func FailureText(loop string, n int, errText string, limit int) string {
	text := fmt.Sprintf("%s failed %d times in a row:\n%s", loop, n, errText)
	if len(text) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return text
}
func RecoveryText(loop string, n int, suffix string) string {
	text := loop + suffix
	if n > 0 {
		text += fmt.Sprintf(" after %d failures in a row", n)
	}
	return text
}
