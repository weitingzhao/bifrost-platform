package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// PipelineRun is a completed external pipeline run.
type PipelineRun struct {
	Run, UID, Pipeline, Kind, Result, Repo, Revision, Reason, Tree, TestedBy string
	CreatedAt, StartedAt, CompletedAt                                        time.Time
}

func pipelineSeenBody(p PipelineRun) map[string]any {
	b := map[string]any{"run": p.Run, "uid": p.UID, "pipeline": p.Pipeline, "kind": p.Kind, "result": p.Result,
		"created_at": p.CreatedAt.UTC().Format(time.RFC3339), "started_at": p.StartedAt.UTC().Format(time.RFC3339), "completed_at": p.CompletedAt.UTC().Format(time.RFC3339)}
	for k, v := range map[string]string{"repo": p.Repo, "revision": p.Revision, "reason": p.Reason, "tree": p.Tree, "tested_by": p.TestedBy} {
		if v != "" {
			b[k] = v
		}
	}
	return b
}

// RecordExternal records changed external facts, continuing after invalid facts.
// The initial batch read avoids taking locks for facts that have not changed;
// each changed fact is checked again under its advisory lock.
func RecordExternal(ctx context.Context, db DB, kind string, facts []map[string]any) (int, error) {
	keys := map[string][]string{"approval_seen": {"approval"}, "question_seen": {"question"}, "pipeline_seen": {"run", "uid"}}[kind]
	if len(keys) == 0 {
		return 0, fmt.Errorf("%w: unsupported external kind %q", ErrInvalid, kind)
	}
	type fact struct {
		body     map[string]any
		key, doc string
	}
	var valid []fact
	var errs []error
	for i, b := range facts {
		if err := checkBody(kind, b); err != nil {
			errs = append(errs, fmt.Errorf("fact %d: %w", i, err))
			continue
		}
		k := map[string]any{}
		for _, name := range keys {
			k[name] = b[name]
		}
		kd, e := canon.Encode(k)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		doc, e := canon.Encode(b)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		valid = append(valid, fact{b, string(kd), string(doc)})
	}
	if len(valid) == 0 {
		return 0, errors.Join(errs...)
	}
	latest := map[string]string{}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		args := []any{kind}
		predicates := make([]string, len(valid))
		for i, f := range valid {
			args = append(args, f.key)
			predicates[i] = fmt.Sprintf("body_json @> $%d::jsonb", i+2)
		}
		expr := make([]string, len(keys))
		for i, k := range keys {
			expr[i] = "body_json->'" + k + "'"
		}
		distinct := strings.Join(expr, ",")
		rows, e := tx.Query(ctx, "SELECT DISTINCT ON ("+distinct+") body_json::text FROM agentrt.event WHERE event_type=$1 AND ("+strings.Join(predicates, " OR ")+") ORDER BY "+distinct+", event_id DESC", args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				return e
			}
			v, e := canon.Parse([]byte(raw))
			if e != nil {
				return e
			}
			b := v.(map[string]any)
			k := map[string]any{}
			for _, name := range keys {
				k[name] = b[name]
			}
			kd, e := canon.Encode(k)
			if e != nil {
				return e
			}
			doc, e := canon.Encode(b)
			if e != nil {
				return e
			}
			latest[string(kd)] = string(doc)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, errors.Join(append(errs, err)...)
	}
	n := 0
	for _, f := range valid {
		if latest[f.key] == f.doc {
			continue
		}
		recorded, e := seen(ctx, db, kind, func() map[string]any {
			k := map[string]any{}
			for _, name := range keys {
				k[name] = f.body[name]
			}
			return k
		}(), func(context.Context) (map[string]any, bool, error) { return f.body, true, nil })
		if e != nil {
			errs = append(errs, e)
			continue
		}
		if recorded {
			n++
		}
		latest[f.key] = f.doc
	}
	return n, errors.Join(errs...)
}

// Body returns the canonical approval_seen fields.
func (a ApprovalFact) Body() map[string]any { return approvalSeenBody(a) }

// Body returns the canonical pipeline_seen fields.
func (p PipelineRun) Body() map[string]any { return pipelineSeenBody(p) }

// Body returns the canonical question_seen fields, refusing partial answers.
func (q Question) Body() (map[string]any, error) {
	b, _, err := questionSeenBody(q.ID, q)
	return b, err
}

// RecordCIReuse records an accepted reuse once across readers and retries.
// reused_run and reused_uid identify the original green run.
func RecordCIReuse(ctx context.Context, db DB, repo, revision, tree, run, uid string) (bool, error) {
	body := map[string]any{"repo": repo, "revision": revision, "tree": tree, "reused_run": run, "reused_uid": uid}
	return seen(ctx, db, "ci_reused", body, func(context.Context) (map[string]any, bool, error) { return body, true, nil })
}
