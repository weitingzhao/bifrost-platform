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
