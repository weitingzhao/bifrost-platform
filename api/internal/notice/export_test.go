package notice

// OracleRedact is main's redactor at commit 8b3259a. The push-text property
// test uses it as the oracle. It is not part of the library.
func OracleRedact(s string) string { return redactAt8b3259a(s) }
