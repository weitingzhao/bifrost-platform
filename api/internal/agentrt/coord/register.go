package coord

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"

// Where a message's body is stored (artifact.PutArtifact) and the record of a
// text that claims an approval (AlertClaim) are registered here, in the init
// of the package that uses them: the store no longer does it for coord.
func init() { Register(artifact.PutArtifact, AlertClaim) }
