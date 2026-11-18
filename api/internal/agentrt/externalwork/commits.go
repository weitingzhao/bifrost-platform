package externalwork

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// CommitNamed is the external work that named a commit: the work, who
// registered it, the full commit, when the work's earliest event carrying it
// was recorded, the step and task it is bound to now (its latest note: a
// heartbeat may name the commit before the work is linked), and the
// repository its description names. RepoUnread: the description could not be
// read, so Repo is empty and unknown.
//
// This is what the lineage domain offers the delivery read of a commit
// (deliveryreadings.ReadDelivery, through its CommitNaming port).
type CommitNamed struct {
	Work, Registrant, Commit string
	At                       time.Time
	Task, Node, Repo         string
	RepoUnread               bool
}

// NamingOfCommit finds the external work naming a commit starting with
// prefix: the earliest event of any work that carries it, a heartbeat or the
// submitted event (a work's note keeps every commit named so far, so each work
// counts once per commit, at its first event carrying it). Two different
// commits under one prefix are eventlog.ErrInvalid naming both; none is
// pgx.ErrNoRows. It reads in the caller's transaction.
func NamingOfCommit(ctx context.Context, tx pgx.Tx, prefix string) (CommitNamed, error) {
	rows, err := tx.Query(ctx, `SELECT body, sha, at FROM (SELECT DISTINCT ON (e.event_stream_id, s.sha)
	  e.event_id, e.body_json->'external_work' AS body, s.sha, e.at FROM agentrt.event e
	  CROSS JOIN LATERAL jsonb_array_elements_text(e.body_json->'external_work'->'commit_shas') AS s(sha)
	  WHERE e.event_stream_id LIKE 'external-work:%' AND e.event_type IN ('external_work_noted', 'note')
	  AND jsonb_typeof(e.body_json->'external_work'->'commit_shas') = 'array'
	  AND left(s.sha, length($1)) = $1 ORDER BY e.event_stream_id, s.sha, e.seq) x
	  ORDER BY event_id, sha LIMIT 50`, prefix)
	if err != nil {
		return CommitNamed{}, err
	}
	defer rows.Close()
	var best struct {
		note   ExternalNote
		commit string
		at     time.Time
	}
	found := false
	for rows.Next() {
		var raw []byte
		var commit string
		var at time.Time
		if err = rows.Scan(&raw, &commit, &at); err != nil {
			return CommitNamed{}, err
		}
		if found {
			if commit != best.commit {
				return CommitNamed{}, fmt.Errorf("%w: commit %s is ambiguous: external work %s (%s) and (%s)", eventlog.ErrInvalid, prefix, best.note.ID, best.commit, commit)
			}
			continue
		}
		if err = json.Unmarshal(raw, &best.note); err != nil {
			return CommitNamed{}, err
		}
		best.commit, best.at, found = commit, at, true
	}
	if err = rows.Err(); err != nil {
		return CommitNamed{}, err
	}
	if !found {
		return CommitNamed{}, pgx.ErrNoRows
	}
	rows.Close()
	out := CommitNamed{Work: best.note.ID, Registrant: best.note.Actor, Commit: best.commit, At: best.at}
	note, _, _, err := ReadExternal(ctx, tx, best.note.ID)
	if err != nil {
		return CommitNamed{}, err
	}
	out.Task, out.Node = note.Task, note.Node
	d, err := ExternalDescriptionOf(ctx, tx, note.Artifact)
	switch {
	case err == nil:
		out.Repo = d.Repo
	case ctx.Err() != nil:
		return CommitNamed{}, err
	default:
		out.RepoUnread = true
	}
	return out, nil
}
