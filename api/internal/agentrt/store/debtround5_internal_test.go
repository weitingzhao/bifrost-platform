package store

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestReviewR5OfflineMoveEndpoints(t *testing.T) {
	t.Parallel()
	for _, ends := range [][2]any{{nil, nil}, {nil, "doing"}, {"open", nil}, {"nowhere", "doing"}} {
		b := map[string]any{"debt_id": "TDS-980", "debt_work_id": 980020, "from": ends[0], "to": ends[1]}
		if err := checkBody("debt_work_linked", b); err == nil {
			t.Errorf("accepted endpoints %v", ends)
		}
	}
}

// This compares source literals, not PostgreSQL execution of btrim.
func TestReviewR5OfflineWhitespaceSet(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../migrations/0011_debts.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`SELECT btrim\(body, E'([^']*)'\)`).FindSubmatch(raw)
	if len(m) != 2 {
		t.Fatal("whitespace literal not found")
	}
	literal, err := strconv.Unquote(`"` + string(m[1]) + `"`)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if strings.ContainsRune(literal, r) != unicode.IsSpace(r) {
			t.Fatalf("SQL source and Go differ at U+%04X", r)
		}
		if unicode.IsSpace(r) {
			count++
		}
	}
	t.Logf("SQL literal matches all %d unicode.IsSpace code points", count)
}
