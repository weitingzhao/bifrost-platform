package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
)

// A batch's operation records name its final R, while Git retains one
// replayed commit per member. Only the complete recorded stack proves those
// intermediate commits; a matching trailer alone is insufficient.
func stgCommitMatchesLanding(s StgState, commits []landing.ReleaseCommit, index int) bool {
	op := landinggit.LandedOperation(commits[index].Message)
	o := s.Legacy.Operations[op]
	for _, b := range s.LandingBatches {
		position := -1
		for i, member := range b.Members {
			if member == op {
				position = i
			}
		}
		if position < 0 {
			continue
		}
		start := index - position
		if start < 0 || start+len(b.Members) > len(commits) {
			return false
		}
		parent := b.M0
		for i, member := range b.Members {
			c := commits[start+i]
			m := s.Legacy.Operations[member]
			if !m.EverLanded || m.DryRun || m.R != b.R || m.M0 != b.M0 || landinggit.LandedOperation(c.Message) != member || !landinggit.HasTaskTrailer(c.Message, m.Task) || len(c.Parents) != 1 || c.Parents[0] != parent {
				return false
			}
			parent = c.SHA
		}
		return parent == b.R
	}
	return o.R == commits[index].SHA
}
