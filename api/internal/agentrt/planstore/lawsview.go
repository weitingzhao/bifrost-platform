package planstore

import (
	"context"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The register as the Owner reads it (closed loop, standard 2: on a page he
// reads how many standards of a law are reached, missed and unmeasured).
//
// The audit line writes one conclusion per standard's step as a note
// (plannote.go): 「结论 s1：达到|部分|没达到|没核｜标准版本 appr_…｜读数 … @UTC」.
// Such a note is also recorded as plan_step_judged on the step, so the
// latest judgement of each step is read by key (eventlog.LatestByKey). The
// four grades fold into three numbers as the audit line set (2026-11-24):
// 达到 is reached; 部分 and 没达到 are missed; 没核, or no conclusion, is
// unmeasured.

var judgementLine = regexp.MustCompile(`^\s*结论\s*(s[0-9A-Za-z_-]+)\s*[:：]\s*(达到|部分|没达到|没核)`)
var versionRef = regexp.MustCompile(`标准版本\s*(appr_[0-9a-f]{16})`)

func recordJudgement(ctx context.Context, tx pgx.Tx, root, step string, note int64, text string) error {
	m := judgementLine.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	body := map[string]any{"step": step, "standard": m[1], "grade": m[2], "note_event_id": note}
	if v := versionRef.FindStringSubmatch(text); v != nil {
		body["version"] = v[1]
	}
	_, err := appendPlan(ctx, tx, eventlog.Event{Stream: PlanStream(root), Type: "plan_step_judged", Actor: "controller", PlanNode: step,
		Cause: note, Body: body})
	return err
}

// LawStep is one standard of a law as its plan step and latest judgement
// stand.
type LawStep struct {
	Node    string `json:"plan_node_id"`
	Title   string `json:"title"`
	Grade   string `json:"grade,omitempty"` // 达到, 部分, 没达到, 没核; empty when not judged
	Version string `json:"version,omitempty"`
}

// LawCounts are the three numbers of a law's standards.
type LawCounts struct {
	Reached    int `json:"reached"`
	Missed     int `json:"missed"`
	Unmeasured int `json:"unmeasured"`
}

// LawView is a law as the register holds it, with the plan item of its
// current version (when an approved card made one) and its numbers.
type LawView struct {
	Law
	Item   string     `json:"plan_item,omitempty"`
	Counts *LawCounts `json:"counts,omitempty"`
	Steps  []LawStep  `json:"steps,omitempty"`
}

// LawsView is the register, by number, with each law's numbers.
func LawsView(ctx context.Context, db eventlog.DB) ([]LawView, error) {
	var out []LawView
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		laws, err := currentLaws(ctx, tx)
		if err != nil {
			return err
		}
		judged, err := eventlog.LatestByKey(ctx, tx, "plan_step_judged", "step", time.Time{})
		if err != nil {
			return err
		}
		grade := map[string]LawStep{}
		for _, r := range judged {
			s := func(k string) string { v, _ := r.Body[k].(string); return v }
			grade[s("step")] = LawStep{Grade: s("grade"), Version: s("version")}
		}
		for _, l := range laws {
			v := LawView{Law: l}
			if l.ApprovalID != "" {
				var item string
				err := tx.QueryRow(ctx, `SELECT plan_node_id FROM agentrt.plan_node WHERE node_kind = 'item' AND strpos(title, $1) > 0
				  ORDER BY created_at DESC LIMIT 1`, l.ApprovalID).Scan(&item)
				if err != nil && err != pgx.ErrNoRows {
					return err
				}
				if item != "" {
					v.Item, v.Counts = item, &LawCounts{}
					rows, err := tx.Query(ctx, `SELECT plan_node_id, title FROM agentrt.plan_node WHERE parent_plan_node_id = $1
					  AND node_kind = 'step' AND state IS DISTINCT FROM 'dropped' ORDER BY ordinal`, item)
					if err != nil {
						return err
					}
					for rows.Next() {
						var st LawStep
						if err := rows.Scan(&st.Node, &st.Title); err != nil {
							rows.Close()
							return err
						}
						if g, ok := grade[st.Node]; ok {
							st.Grade, st.Version = g.Grade, g.Version
						}
						switch st.Grade {
						case "达到":
							v.Counts.Reached++
						case "部分", "没达到":
							v.Counts.Missed++
						default:
							v.Counts.Unmeasured++
						}
						v.Steps = append(v.Steps, st)
					}
					rows.Close()
					if err := rows.Err(); err != nil {
						return err
					}
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}
