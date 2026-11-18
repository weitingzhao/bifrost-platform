package planreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// Adoption is the stored view description; reading it grants no authority.
type Adoption struct {
	V        int               `json:"v"`
	Kind     string            `json:"kind"`
	Root     string            `json:"root"`
	Tenant   string            `json:"tenant"`
	Version  int               `json:"plan_version"`
	Artifact string            `json:"artifact_id"`
	Nodes    map[string]string `json:"nodes"`
	Decision int64             `json:"decided_event_id"`
}

func AdoptedViews(ctx context.Context, tx pgx.Tx) (map[string]Adoption, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (event_stream_id) body_json->'plan_view' FROM agentrt.event WHERE event_type IN ('plan_view_adopted','note') AND body_json ? 'plan_view' ORDER BY event_stream_id, seq DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Adoption{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var a Adoption
		if err = json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out[a.Root] = a
	}
	return out, rows.Err()
}
