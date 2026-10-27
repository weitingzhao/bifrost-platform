package landinggit

import (
	"regexp"
	"strings"
)

var cherryPickLine = regexp.MustCompile(`^\(cherry picked from commit ([0-9a-fA-F]{40})\)$`)

// CherryPickSource accepts exactly one standalone provenance line in the
// terminal trailer area. Body text, quotations and code examples are not
// provenance. Both local Git and Gitea use this parser.
func CherryPickSource(message string) string {
	area := historyTrailerArea(message)
	source := ""
	for _, line := range strings.Split(area, "\n") {
		if m := cherryPickLine.FindStringSubmatch(line); m != nil {
			if source != "" {
				return ""
			}
			source = strings.ToLower(m[1])
		}
	}
	return source
}

// HasTaskTrailer requires the binding's task, not a Change-Id borrowed from
// the candidate being checked. Duplicate task trailers are ambiguous.
func HasTaskTrailer(message, task string) bool {
	if task == "" {
		return false
	}
	count, taskLine := 0, false
	for _, line := range strings.Split(historyTrailers(message), "\n") {
		if taskLine && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			return false // a continued value is not exactly the bound task
		}
		key, value, _ := strings.Cut(line, ":")
		taskLine = strings.EqualFold(key, "Agentrt-Task")
		if taskLine {
			count++
			if count > 1 || strings.TrimSpace(value) != task {
				return false
			}
		}
	}
	return count == 1
}

func historyTrailers(message string) string {
	lines := []string{}
	for _, line := range strings.Split(historyTrailerArea(message), "\n") {
		if !cherryPickLine.MatchString(line) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// Git can put -x provenance in its own final paragraph, before or after
// ordinary trailers. Walk only the contiguous suffix of trailer paragraphs,
// excluding the subject and stopping at the body. Preserve whitespace so an
// indented/quoted marker cannot become a standalone provenance line.
func historyTrailerArea(message string) string {
	message = strings.TrimRight(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	paragraphs := strings.Split(message, "\n\n")
	start := len(paragraphs)
	for i := len(paragraphs) - 1; i > 0; i-- {
		if !historyTrailerParagraph(paragraphs[i]) {
			break
		}
		start = i
	}
	return strings.Join(paragraphs[start:], "\n")
}

func historyTrailerParagraph(block string) bool {
	foldable := false
	for _, line := range strings.Split(block, "\n") {
		if cherryPickLine.MatchString(line) {
			foldable = false
			continue
		}
		if foldable && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || strings.TrimSpace(value) == "" || strings.ContainsAny(key, " \t") {
			return false
		}
		foldable = true
	}
	return true
}
