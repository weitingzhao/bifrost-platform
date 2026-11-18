package planstore

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
// never verified facts, and never cross the bound step or dev tenant. work is
// the root's registered work as the external work port gave it to this view.
func planEvidenceCandidates(ctx context.Context, tx pgx.Tx, snap *planFactsSnapshot, root string, work []map[string]any) (map[string][]candidateEvidence, error) {
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
	steps, ids, labels, sources := []string{}, []string{}, []string{}, []string{}
	for _, w := range work {
		field := func(name string) string { s, _ := w[name].(string); return s }
		step := field("plan_node_id")
		if _, bound := out[step]; !bound || field("state") != "Submitted" {
			continue
		}
		for _, c := range []struct{ id, label, source string }{
			{field("report_digest"), "Submitted report", "external_work.report"},
			{field("artifact_id"), "Work registration", "external_work.registration"},
		} {
			if c.id != "" {
				steps, ids = append(steps, step), append(ids, c.id)
				labels = append(labels, c.label+" · "+field("source_run_key"))
				sources = append(sources, c.source+":"+field("external_work_id"))
			}
		}
	}
	// Only what is still stored is offered, in the database's order.
	rows, err := tx.Query(ctx, `SELECT c.step,c.id,c.label,c.source FROM unnest($1::text[],$2::text[],$3::text[],$4::text[]) c(step,id,label,source)
 JOIN agentrt.artifact a ON a.artifact_id=c.id ORDER BY c.step,c.id,c.source`, steps, ids, labels, sources)
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
