package landing

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"

// ReleaseCommit is a commit read from the complete first-parent range, oldest
// first. Parents and the full message are evidence, not executor summaries.
type ReleaseCommit struct {
	SHA, Message string
	Parents      []string
	Changes      []rules.LandingChange
}
