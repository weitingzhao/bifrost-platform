package coord

import (
	"encoding/json"
	"regexp"
	"strings"
)

var envelopeMessageID = regexp.MustCompile(`^m_[0-9A-HJKMNP-TV-Z]{26}$`)

// replyReference projects only the existing v1 envelope's sender-declared link.
// It publishes no body and grants no read or acknowledgement ability.
// Observers must also check participants before calling it a reply.
func replyReference(body []byte) string {
	var envelope struct {
		Version int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
		ReplyTo string `json:"reply_to"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Version != 1 || envelope.Type == "" || envelope.Summary == "" || !envelopeMessageID.MatchString(envelope.ReplyTo) {
		return ""
	}
	return envelope.ReplyTo
}

// MaxLetterSummary is how many characters of a letter's summary go on
// message_posted.
const MaxLetterSummary = 140

// letterSummary is the one line the sender declared as the v1 envelope's
// summary, folded to one line and cut to MaxLetterSummary characters, for
// message_posted (2026-11-24: cockpit asked for it so a timeline card can say
// what a letter is about). Only what the sender wrote as the letter's subject
// is published; the text stays the receiver's to read (TD-370), so a letter
// with no envelope publishes nothing rather than its first line.
func letterSummary(body []byte) string {
	var envelope struct {
		Version int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Version != 1 || envelope.Type == "" {
		return ""
	}
	line := strings.Join(strings.Fields(envelope.Summary), " ")
	if r := []rune(line); len(r) > MaxLetterSummary {
		line = string(r[:MaxLetterSummary-1]) + "…"
	}
	return line
}
