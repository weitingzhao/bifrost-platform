// Package mainhistory is the answer to one question about a result: is it on
// main, and in effect there. It holds the answer's types and nothing else:
// no reader of Git, no judgement, no storage. The reader that works the
// answer out is verification's (landinggit); the plan's completion atoms read
// it; both name these types and neither names the other.
package mainhistory

// History is where a result stands in main's history.
type History string

const (
	Unknown   History = "unknown"
	Never     History = "never"
	Effective History = "effective"
	Reverted  History = "reverted"
)

// MainHistory is the answer for one result.
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
