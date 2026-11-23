package coord

import (
	"encoding/json"
	"regexp"
)

var envelopeMessageID = regexp.MustCompile(`^m_[0-9A-HJKMNP-TV-Z]{26}$`)

// replyReference projects only the existing v1 envelope's sender-declared link.
// It publishes no body or summary and grants no read or acknowledgement ability.
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
