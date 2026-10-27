package store

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type candidateEvidence struct {
	Artifact string `json:"artifact_id"`
	Label    string `json:"label"`
	Source   string `json:"source"`
}

// Read with the plan's repeatable-read transaction. Candidates are suggestions,
// never verified facts, and never cross the bound step or dev tenant.
func planEvidenceCandidates(ctx context.Context, tx pgx.Tx, snap *planFactsSnapshot, root string) (map[string][]candidateEvidence, error) {
	out := map[string][]candidateEvidence{}
	if d := snap.Docs[root]; d != nil {
		bs, _ := d.Bindings()
		for _, b := range bs {
			if b.Source == "owner_signoff" {
				out[b.Step] = []candidateEvidence{}
			}
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	steps := make([]string, 0, len(out))
	for step := range out {
		steps = append(steps, step)
	}
	rows, err := tx.Query(ctx, `WITH external AS (
 SELECT DISTINCT ON(event_stream_id) body_json->'external_work' e
 FROM agentrt.event WHERE event_type='note' AND body_json ? 'external_work'
 ORDER BY event_stream_id,seq DESC
 ), candidates AS (
 SELECT e->>'plan_node_id' step, x.id, x.label || ' · ' || (e->>'source_run_key') label,
 x.source || ':' || (e->>'external_work_id') source
 FROM external CROSS JOIN LATERAL (VALUES (e->>'report_digest','Submitted report','external_work.report'),
 (e->>'artifact_id','Work registration','external_work.registration')) x(id,label,source)
 WHERE e->>'root'=$1 AND e->>'state'='Submitted' AND e->>'plan_node_id'=ANY($2::text[])
 UNION ALL
 SELECT t.plan_node_id,v.evidence_artifact_id, v.method || ' · ' || v.verdict || ' · ' || t.task_id,
 'verification:' || v.verification_id
 FROM agentrt.verification v JOIN agentrt.task t USING(task_id) JOIN agentrt.mission m USING(mission_id)
 WHERE t.plan_node_id=ANY($2::text[]) AND m.tenant='dev'
 ) SELECT c.step,c.id,c.label,c.source FROM candidates c JOIN agentrt.artifact a ON a.artifact_id=c.id
 ORDER BY c.step,c.id,c.source`, root, steps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var step string
		var c candidateEvidence
		if err = rows.Scan(&step, &c.Artifact, &c.Label, &c.Source); err != nil {
			return nil, err
		}
		// One checkbox per artifact, with every provenance retained.
		list := out[step]
		i := slices.IndexFunc(list, func(old candidateEvidence) bool { return old.Artifact == c.Artifact })
		if i >= 0 {
			list[i].Source += "; " + c.Source
		} else {
			label := []rune(strings.Join(strings.Fields(c.Label), " "))
			if len(label) > 120 {
				c.Label = fmt.Sprintf("%s…", string(label[:119]))
			} else {
				c.Label = string(label)
			}
			list = append(list, c)
		}
		out[step] = list
	}
	return out, rows.Err()
}
