package lineage

import (
	"regexp"
	"strings"
)

const (
	keySession    = "Claude-Session"
	keyTranscript = "Claude-Transcript"
	keyChangeID   = "Change-Id"
	keyWork       = "Work"
)

var (
	trailerLine  = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*):\s*(.*\S)\s*$`)
	changeIDLine = regexp.MustCompile(`(?m)^Change-Id:\s*(I[0-9a-f]{40})\s*$`)
)

// trailers holds the lineage keys of one commit message.
type trailers struct {
	session, transcript, changeID string
	work                          string // raw Work: value; empty when the key is absent
	agent                         bool   // a Co-Authored-By line names Claude
}

// parseMessage reads the trailer block (the last paragraph, as git does) for the
// lineage keys. The first value of a key wins: an amend by another session keeps
// the original, matching lineage.sh.
func parseMessage(msg string) (subject string, t trailers) {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	if len(lines) > 0 {
		subject = strings.TrimSpace(lines[0])
	}
	start := len(lines)
	for start > 1 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	if start <= 1 { // a one-paragraph message has no trailer block
		return subject, t
	}
	for _, line := range lines[start:] {
		m := trailerLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], m[2]
		switch {
		case strings.EqualFold(key, keySession) && t.session == "":
			t.session = val
		case strings.EqualFold(key, keyTranscript) && t.transcript == "":
			t.transcript = val
		case strings.EqualFold(key, keyChangeID) && t.changeID == "":
			t.changeID = val
		case strings.EqualFold(key, keyWork) && t.work == "":
			t.work = val
		case strings.EqualFold(key, "Co-Authored-By") && strings.Contains(strings.ToLower(val), "claude"):
			t.agent = true
		}
	}
	return subject, t
}

// changeIDsAnywhere returns every Change-Id line in the whole message: a squash
// keeps each folded commit's Change-Id in the body, outside the trailer block.
func changeIDsAnywhere(msg string) []string {
	var out []string
	for _, m := range changeIDLine.FindAllStringSubmatch(msg, -1) {
		out = append(out, m[1])
	}
	return out
}

// sessionLink opens the thread: desktop ids (local_…) through the Claude app,
// cloud sessions are already a claude.ai URL.
func sessionLink(session string) string {
	switch {
	case strings.HasPrefix(session, "local_"):
		return "claude://claude.ai/epitaxy/" + session
	case strings.HasPrefix(session, "https://claude.ai/"):
		return session
	}
	return ""
}
