package landingci

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Landing grants' CI (W-31 core C-5, coordinator v3 §3.5, review 694 B-01):
// an operation that needs a grant runs CI pinned to its recorded key, and
// every grant read is Read's strict verdict on that key, never the v1
// LandingCI adapter.

func keyOf(k store.GrantCIKey) Key {
	return Key{Repo: k.Repo, Revision: k.Revision, Base: k.Base, Siblings: maps.Clone(k.Siblings)}
}

// GrantCI is the store's grant CI reader: Read on the operation's key, the
// pipeline it verified against, the candidate and sibling results.
func (c *Client) GrantCI(ctx context.Context, key store.GrantCIKey, after time.Time) (store.GrantCIEvidence, error) {
	repo, ok := repository(key.Repo)
	if !ok {
		return store.GrantCIEvidence{}, fmt.Errorf("no landing CI for %s", key.Repo)
	}
	ev, err := c.Read(ctx, keyOf(key), after)
	if err != nil {
		return store.GrantCIEvidence{}, err
	}
	out := store.GrantCIEvidence{Run: ev.Run, UID: ev.UID, CreatedAt: ev.CreatedAt, Status: ev.Status, Reason: ev.Reason,
		Candidate: ev.Results[repo.CandidateResult], Siblings: map[string]string{}}
	for sibling, result := range repo.SiblingResults {
		out.Siblings[sibling] = ev.Results[result]
	}
	// Read verifies the pipeline reference only of a successful run.
	if ev.Status == "Succeeded" && ev.Reason == "" {
		out.Pipeline = repo.Pipeline
	}
	return out, nil
}

// EnsureGrantCI creates the pinned run of a key under the operation's name.
func (c *Client) EnsureGrantCI(ctx context.Context, name string, key store.GrantCIKey) error {
	return c.Ensure(ctx, name, keyOf(key))
}
