package releasepolicy

// StatementsForTest returns each statement's text, including its semicolon.
func StatementsForTest(sql string) ([]string, error) {
	return statementTexts(sql)
}

// MutationPointsForTest returns source indexes where inserting one space
// changes token adjacency, and indexes of ASCII letters that are part of a
// token. Letters inside comments are omitted.
func MutationPointsForTest(sql string) (gaps, letters []int, err error) {
	return mutationPoints(sql)
}

// SeparatorSpansForTest returns source ranges of whitespace and comments
// that sit between two tokens. Replacing one with a comment is not a change.
func SeparatorSpansForTest(sql string) ([][2]int, error) {
	return separatorSpans(sql)
}
