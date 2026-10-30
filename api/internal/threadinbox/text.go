package threadinbox

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// Text redacts before clipping, removes controls, and stays on a UTF-8 boundary.
func Text(s string, limit int) string {
	s = notice.Redact(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		s = s[:limit]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
