package landinggit

import "regexp"

// revertLine is git's revert line, of a commit ("X.") or of a merge ("X, reversing").
var revertLine = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{40})(?:\.\s*$|,\s)`)
