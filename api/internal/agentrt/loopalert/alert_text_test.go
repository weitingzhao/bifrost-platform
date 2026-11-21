package loopalert

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAlertTextFrozenParity(t *testing.T) {
	t.Parallel()
	for _, errText := range []string{"", "failure", strings.Repeat("中文x", 3000)} {
		for _, limit := range []int{0, 1, 17, 4096, 20000} {
			want := fmt.Sprintf("%s failed %d times in a row:\n%s", "loop", 4, errText)
			if len(want) > limit {
				cut := limit
				for cut > 0 && !utf8.RuneStart(want[cut]) {
					cut--
				}
				want = want[:cut] + "…"
			}
			if got := FailureText("loop", 4, errText, limit); got != want {
				t.Fatal("failure text differs")
			}
		}
	}
	for _, n := range []int{-1, 0, 1, 9} {
		want := "loop passes again"
		if n > 0 {
			want += fmt.Sprintf(" after %d failures in a row", n)
		}
		if got := RecoveryText("loop", n, " passes again"); got != want {
			t.Fatal("recovery differs")
		}
	}
}
