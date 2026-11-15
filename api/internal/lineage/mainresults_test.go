package lineage

import "strings"

// sha40 supplies commit identities for the live plan-history tests.
func sha40(c string) string { return strings.Repeat(c, 40) }
