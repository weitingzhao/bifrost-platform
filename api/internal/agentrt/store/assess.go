package store

// AI assessments are advice only; historical stream readers retain these names.
const (
	AssessCard     = "card"
	AssessApproval = "approval"
)

// AssessStream names the item's recorded assessment stream.
func AssessStream(kind, id string) string { return "assess:" + kind + ":" + id }
