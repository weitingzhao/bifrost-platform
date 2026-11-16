package agentthreads

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"

// WorksFromIdentities projects registered work for the thread list. Connection
// readiness and reading the source remain the server adapter's responsibility.
func WorksFromIdentities(ids []workregistration.Identity) []Work {
	out := make([]Work, 0, len(ids))
	for _, x := range ids {
		out = append(out, Work{ID: x.ID, Root: x.Root, Node: x.Node, State: x.State, Host: x.Host, Vendor: x.Vendor, Session: x.Session, Epoch: int(x.Epoch)})
	}
	return out
}
