package landing

import (
	"slices"
)

// Batch landing (R5:5.7, goal 2): the tasks waiting to land on
// bifrost-platform are stacked into one batch that runs one landing CI. A
// batch is several operations, one per root, each claimed as today, all
// prepared with the same M0 (main when the batch was formed) and the same
// R, the tip of the members replayed one on another in batch order (member
// 1 on M0, member k on member k-1's commit). The first member is the lead:
// only it kicks, reruns or parks for CI. Green, R is pushed once to main;
// red, the members the failing tests point at go back for rework, and when
// nothing maps the batch is split in half and tested again. No protocol kind
// is new: the batch itself is a controller note beside the protocol.

// BatchCandidate is a root that may join a batch, with the facts its claim
// would be judged on (read with no operation yet).
type BatchCandidate struct {
	Task  string
	Facts Facts
}

// Split is the first and the second half of members, the first the larger.
func Split[T any](members []T) ([]T, []T) {
	h := (len(members) + 1) / 2
	return slices.Clone(members[:h]), slices.Clone(members[h:])
}

// BatchMember is one member as blame reads it: its operation, its root and
// the paths its result changes (rules.LandingChange paths).
type BatchMember struct {
	Op, Task string
	Paths    []string
}
