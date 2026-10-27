package store

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// Evidence is a set selected at signing; a reordered retry has the same body.
func signoffEvidence(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 20 {
		return nil, invalid("acceptance signoff requires 1..20 evidence artifact ids")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	for i, id := range ids {
		if !strings.HasPrefix(id, "sha256:") || len(id) != 71 || !shaPattern.MatchString(id[7:]) || (i > 0 && ids[i-1] == id) {
			return nil, invalid("invalid or duplicate signoff evidence artifact id")
		}
	}
	return ids, nil
}

func acceptanceSignoffBody(ctx context.Context, tx pgx.Tx, actor, step, ref string, evidence []string) (map[string]any, error) {
	body := map[string]any{"ref": ref}
	if !strings.HasPrefix(ref, "plan-acceptance:") {
		if len(evidence) > 0 {
			return nil, invalid("evidence is only supported for acceptance signoffs")
		}
		return body, nil
	}
	if actor != OwnerActor {
		return nil, ErrNotOwner
	}
	ids, err := signoffEvidence(evidence)
	if err != nil {
		return nil, err
	}
	if err = haveArtifacts(ctx, tx, ids); err != nil {
		return nil, err
	}
	snap, err := readPlanFacts(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, d := range snap.Docs {
		bs, _ := d.Bindings()
		for _, b := range bs {
			if b.Step == step && b.Source == "owner_signoff" && b.SignoffRef() == ref {
				body["binding_sha256"], body["acceptance_sha256"], body["plan_version"], body["evidence"] = b.Digest(), b.AcceptanceSHA256, b.Version, ids
				return body, nil
			}
		}
	}
	return nil, invalid("unadopted acceptance signoff")
}

type acceptanceSignoff struct {
	Binding    string   `json:"binding_sha256"`
	Acceptance string   `json:"acceptance_sha256"`
	Version    int      `json:"plan_version"`
	Evidence   []string `json:"evidence"`
}

func (s acceptanceSignoff) matches(b planview.Binding) bool {
	_, err := signoffEvidence(s.Evidence)
	return err == nil && s.Binding == b.Digest() && s.Acceptance == b.AcceptanceSHA256 && s.Version == b.Version
}
