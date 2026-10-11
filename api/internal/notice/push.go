package notice

// PushText is the only way agent-written text becomes push text. It redacts,
// strips, then redacts again. The first redaction hides a secret while the
// character that marked its edge is still there. The replacement contains
// nothing StripControls removes, so stripping cannot uncover it. The second
// redaction hides a secret that stripping joined, including one that a removed
// character had split in two. keepNewlines is the flag StripControls takes: a
// title passes false, a body passes true. Calling it again with the same flag
// returns the same string.
func PushText(s string, keepNewlines bool) string {
	return Redact(StripControls(Redact(s), keepNewlines))
}
