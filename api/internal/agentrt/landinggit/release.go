package landinggit

import (
	"strings"
)

// LandedOperation accepts one exact, uncontinued PROD trailer in the terminal
// trailer area. A quoted marker in the commit body confers no authority.
func LandedOperation(message string) string {
	op, count, previous := "", 0, false
	for _, line := range strings.Split(historyTrailers(message), "\n") {
		if previous && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			return ""
		}
		key, value, _ := strings.Cut(line, ":")
		previous = strings.EqualFold(key, "Landed-By")
		if previous {
			count++
			value = strings.TrimSpace(value)
			if !strings.HasPrefix(value, "agentrt-prod/") {
				return ""
			}
			op = strings.TrimPrefix(value, "agentrt-prod/")
		}
	}
	if count != 1 || strings.ContainsAny(op, " \t\r\n") {
		return ""
	}
	return op
}
