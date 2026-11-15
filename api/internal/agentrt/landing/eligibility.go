package landing

type History string

const (
	Unknown   History = "unknown"
	Never     History = "never"
	Effective History = "effective"
	Reverted  History = "reverted"
)

type MainHistory struct {
	State  History
	Commit string
	// RevertedBy is the commit whose reversal of Commit is in effect, when
	// State is Reverted (S3 B-02: the reconciler records it).
	RevertedBy string
	// Snapshot is the main commit the answer was read at, when the reader
	// pinned one (plan observations keep it as evidence).
	Snapshot string
}
